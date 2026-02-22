package openai

import (
	"encoding/json"
	"sort"
	"strings"

	openaifmt "ds2api/internal/format/openai"
	"ds2api/internal/util"

	"github.com/google/uuid"
)

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
