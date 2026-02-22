package openai

import (
	"net/http"
	"strings"

	openaifmt "ds2api/internal/format/openai"
	"ds2api/internal/sse"
	streamengine "ds2api/internal/stream"
	"ds2api/internal/util"
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
