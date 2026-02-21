package openai

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	openaifmt "ds2api/internal/format/openai"
	"ds2api/internal/sse"
	streamengine "ds2api/internal/stream"
	"ds2api/internal/util"

	"github.com/google/uuid"
)

type responsesStreamRuntime struct {
	w        http.ResponseWriter
	rc       *http.ResponseController
	canFlush bool

	responseID  string
	model       string
	finalPrompt string
	toolNames   []string

	thinkingEnabled bool
	searchEnabled   bool

	bufferToolContent    bool
	emitEarlyToolDeltas  bool
	toolCallsEmitted     bool
	toolCallsDoneEmitted bool

	sieve             toolStreamSieveState
	thinkingSieve     toolStreamSieveState
	thinking          strings.Builder
	text              strings.Builder
	streamToolCallIDs map[int]string
	streamFunctionIDs map[int]string
	functionDone      map[int]bool
	toolCallsDoneSigs map[string]bool
	reasoningItemID   string

	persistResponse func(obj map[string]any)
}

func newResponsesStreamRuntime(
	w http.ResponseWriter,
	rc *http.ResponseController,
	canFlush bool,
	responseID string,
	model string,
	finalPrompt string,
	thinkingEnabled bool,
	searchEnabled bool,
	toolNames []string,
	bufferToolContent bool,
	emitEarlyToolDeltas bool,
	persistResponse func(obj map[string]any),
) *responsesStreamRuntime {
	return &responsesStreamRuntime{
		w:                   w,
		rc:                  rc,
		canFlush:            canFlush,
		responseID:          responseID,
		model:               model,
		finalPrompt:         finalPrompt,
		thinkingEnabled:     thinkingEnabled,
		searchEnabled:       searchEnabled,
		toolNames:           toolNames,
		bufferToolContent:   bufferToolContent,
		emitEarlyToolDeltas: emitEarlyToolDeltas,
		streamToolCallIDs:   map[int]string{},
		streamFunctionIDs:   map[int]string{},
		functionDone:        map[int]bool{},
		toolCallsDoneSigs:   map[string]bool{},
		persistResponse:     persistResponse,
	}
}

func (s *responsesStreamRuntime) sendEvent(event string, payload map[string]any) {
	b, _ := json.Marshal(payload)
	_, _ = s.w.Write([]byte("event: " + event + "\n"))
	_, _ = s.w.Write([]byte("data: "))
	_, _ = s.w.Write(b)
	_, _ = s.w.Write([]byte("\n\n"))
	if s.canFlush {
		_ = s.rc.Flush()
	}
}

func (s *responsesStreamRuntime) sendCreated() {
	s.sendEvent("response.created", openaifmt.BuildResponsesCreatedPayload(s.responseID, s.model))
}

func (s *responsesStreamRuntime) sendDone() {
	_, _ = s.w.Write([]byte("data: [DONE]\n\n"))
	if s.canFlush {
		_ = s.rc.Flush()
	}
}

func (s *responsesStreamRuntime) finalize() {
	finalThinking := s.thinking.String()
	finalText := s.text.String()
	if strings.TrimSpace(finalThinking) != "" {
		s.sendEvent("response.reasoning_text.done", openaifmt.BuildResponsesReasoningTextDonePayload(s.responseID, s.ensureReasoningItemID(), 0, 0, finalThinking))
	}
	if s.bufferToolContent {
		s.processToolStreamEvents(flushToolSieve(&s.sieve, s.toolNames), true)
		s.processToolStreamEvents(flushToolSieve(&s.thinkingSieve, s.toolNames), false)
	}
	// Compatibility fallback: some streams only emit incremental tool deltas.
	// Ensure final function_call_arguments.done is emitted at least once.
	if s.toolCallsEmitted {
		detected := util.ParseToolCalls(finalText, s.toolNames)
		if len(detected) == 0 {
			detected = util.ParseToolCalls(finalThinking, s.toolNames)
		}
		if len(detected) > 0 {
			if !s.toolCallsDoneEmitted {
				s.emitToolCallsDone(detected)
			} else {
				s.emitFunctionCallDoneEvents(detected)
			}
		}
	}

	obj := openaifmt.BuildResponseObject(s.responseID, s.model, s.finalPrompt, finalThinking, finalText, s.toolNames)
	if s.toolCallsEmitted {
		s.alignCompletedOutputCallIDs(obj)
	}
	if s.toolCallsEmitted {
		obj["status"] = "completed"
	}
	if s.persistResponse != nil {
		s.persistResponse(obj)
	}
	s.sendEvent("response.completed", openaifmt.BuildResponsesCompletedPayload(obj))
	s.sendDone()
}

func (s *responsesStreamRuntime) onParsed(parsed sse.LineResult) streamengine.ParsedDecision {
	if !parsed.Parsed {
		return streamengine.ParsedDecision{}
	}
	if parsed.ContentFilter || parsed.ErrorMessage != "" || parsed.Stop {
		return streamengine.ParsedDecision{Stop: true}
	}

	contentSeen := false
	for _, p := range parsed.Parts {
		if p.Text == "" {
			continue
		}
		if p.Type != "thinking" && s.searchEnabled && sse.IsCitation(p.Text) {
			continue
		}
		contentSeen = true
		if p.Type == "thinking" {
			if !s.thinkingEnabled {
				continue
			}
			s.thinking.WriteString(p.Text)
			s.sendEvent("response.reasoning.delta", openaifmt.BuildResponsesReasoningDeltaPayload(s.responseID, p.Text))
			s.sendEvent("response.reasoning_text.delta", openaifmt.BuildResponsesReasoningTextDeltaPayload(s.responseID, s.ensureReasoningItemID(), 0, 0, p.Text))
			if s.bufferToolContent {
				s.processToolStreamEvents(processToolSieveChunk(&s.thinkingSieve, p.Text, s.toolNames), false)
			}
			continue
		}

		s.text.WriteString(p.Text)
		if !s.bufferToolContent {
			s.sendEvent("response.output_text.delta", openaifmt.BuildResponsesTextDeltaPayload(s.responseID, p.Text))
			continue
		}
		s.processToolStreamEvents(processToolSieveChunk(&s.sieve, p.Text, s.toolNames), true)
	}

	return streamengine.ParsedDecision{ContentSeen: contentSeen}
}

func (s *responsesStreamRuntime) processToolStreamEvents(events []toolStreamEvent, emitContent bool) {
	for _, evt := range events {
		if emitContent && evt.Content != "" {
			s.sendEvent("response.output_text.delta", openaifmt.BuildResponsesTextDeltaPayload(s.responseID, evt.Content))
		}
		if len(evt.ToolCallDeltas) > 0 {
			if !s.emitEarlyToolDeltas {
				continue
			}
			formatted := formatIncrementalStreamToolCallDeltas(evt.ToolCallDeltas, s.streamToolCallIDs)
			if len(formatted) == 0 {
				continue
			}
			s.toolCallsEmitted = true
			s.sendEvent("response.output_tool_call.delta", openaifmt.BuildResponsesToolCallDeltaPayload(s.responseID, formatted))
			s.emitFunctionCallDeltaEvents(evt.ToolCallDeltas)
		}
		if len(evt.ToolCalls) > 0 {
			s.emitToolCallsDone(evt.ToolCalls)
		}
	}
}

func (s *responsesStreamRuntime) emitToolCallsDone(calls []util.ParsedToolCall) {
	if len(calls) == 0 {
		return
	}
	sig := toolCallListSignature(calls)
	if sig != "" && s.toolCallsDoneSigs[sig] {
		return
	}
	if sig != "" {
		s.toolCallsDoneSigs[sig] = true
	}
	formatted := formatFinalStreamToolCallsWithStableIDs(calls, s.streamToolCallIDs)
	if len(formatted) == 0 {
		return
	}
	s.toolCallsEmitted = true
	s.toolCallsDoneEmitted = true
	s.sendEvent("response.output_tool_call.done", openaifmt.BuildResponsesToolCallDonePayload(s.responseID, formatted))
	s.emitFunctionCallDoneEvents(calls)
}

func (s *responsesStreamRuntime) ensureReasoningItemID() string {
	if strings.TrimSpace(s.reasoningItemID) != "" {
		return s.reasoningItemID
	}
	s.reasoningItemID = "rs_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	return s.reasoningItemID
}

func (s *responsesStreamRuntime) ensureFunctionItemID(index int) string {
	if id, ok := s.streamFunctionIDs[index]; ok && strings.TrimSpace(id) != "" {
		return id
	}
	id := "fc_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	s.streamFunctionIDs[index] = id
	return id
}

func (s *responsesStreamRuntime) ensureToolCallID(index int) string {
	if id, ok := s.streamToolCallIDs[index]; ok && strings.TrimSpace(id) != "" {
		return id
	}
	id := "call_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	s.streamToolCallIDs[index] = id
	return id
}

func (s *responsesStreamRuntime) functionOutputBaseIndex() int {
	if strings.TrimSpace(s.thinking.String()) != "" {
		return 1
	}
	return 0
}

func (s *responsesStreamRuntime) emitFunctionCallDeltaEvents(deltas []toolCallDelta) {
	for _, d := range deltas {
		if strings.TrimSpace(d.Arguments) == "" {
			continue
		}
		outputIndex := s.functionOutputBaseIndex() + d.Index
		itemID := s.ensureFunctionItemID(outputIndex)
		callID := s.ensureToolCallID(d.Index)
		s.sendEvent(
			"response.function_call_arguments.delta",
			openaifmt.BuildResponsesFunctionCallArgumentsDeltaPayload(s.responseID, itemID, outputIndex, callID, d.Arguments),
		)
	}
}

func (s *responsesStreamRuntime) emitFunctionCallDoneEvents(calls []util.ParsedToolCall) {
	base := s.functionOutputBaseIndex()
	for idx, tc := range calls {
		if strings.TrimSpace(tc.Name) == "" {
			continue
		}
		outputIndex := base + idx
		if s.functionDone[outputIndex] {
			continue
		}
		itemID := s.ensureFunctionItemID(outputIndex)
		callID := s.ensureToolCallID(idx)
		argsBytes, _ := json.Marshal(tc.Input)
		s.sendEvent(
			"response.function_call_arguments.done",
			openaifmt.BuildResponsesFunctionCallArgumentsDonePayload(s.responseID, itemID, outputIndex, callID, tc.Name, string(argsBytes)),
		)
		s.functionDone[outputIndex] = true
	}
}

func (s *responsesStreamRuntime) alignCompletedOutputCallIDs(obj map[string]any) {
	if obj == nil || len(s.streamToolCallIDs) == 0 {
		return
	}
	output, _ := obj["output"].([]any)
	if len(output) == 0 {
		return
	}
	indices := make([]int, 0, len(s.streamToolCallIDs))
	for idx := range s.streamToolCallIDs {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	ordered := make([]string, 0, len(indices))
	for _, idx := range indices {
		id := strings.TrimSpace(s.streamToolCallIDs[idx])
		if id == "" {
			continue
		}
		ordered = append(ordered, id)
	}
	if len(ordered) == 0 {
		return
	}

	functionIdx := 0
	for _, item := range output {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		typ, _ := m["type"].(string)
		switch typ {
		case "function_call":
			if functionIdx < len(ordered) {
				m["call_id"] = ordered[functionIdx]
				functionIdx++
			}
		case "tool_calls":
			tcArr, _ := m["tool_calls"].([]any)
			for i, raw := range tcArr {
				tc, _ := raw.(map[string]any)
				if tc == nil {
					continue
				}
				if i < len(ordered) {
					tc["id"] = ordered[i]
				}
			}
		}
	}
}

func toolCallListSignature(calls []util.ParsedToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	var b strings.Builder
	for i, tc := range calls {
		if i > 0 {
			b.WriteString("|")
		}
		b.WriteString(strings.TrimSpace(tc.Name))
		b.WriteString(":")
		args, _ := json.Marshal(tc.Input)
		b.Write(args)
	}
	return b.String()
}
