package openai

import (
	"encoding/json"
	"sort"
	"strings"

	openaifmt "ds2api/internal/format/openai"
	"ds2api/internal/util"

	"github.com/google/uuid"
)

func (s *responsesStreamRuntime) ensureMessageItemID() string {
	if strings.TrimSpace(s.messageItemID) != "" {
		return s.messageItemID
	}
	s.messageItemID = "msg_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	return s.messageItemID
}

func (s *responsesStreamRuntime) messageOutputIndex() int {
	if strings.TrimSpace(s.thinking.String()) != "" {
		return 1
	}
	return 0
}

func (s *responsesStreamRuntime) ensureMessageItemAdded() {
	if s.messageAdded {
		return
	}
	itemID := s.ensureMessageItemID()
	item := map[string]any{
		"id":     itemID,
		"type":   "message",
		"role":   "assistant",
		"status": "in_progress",
	}
	s.sendEvent(
		"response.output_item.added",
		openaifmt.BuildResponsesOutputItemAddedPayload(s.responseID, itemID, s.messageOutputIndex(), item),
	)
	s.messageAdded = true
}

func (s *responsesStreamRuntime) ensureMessageContentPartAdded() {
	if s.messagePartAdded {
		return
	}
	s.ensureMessageItemAdded()
	s.sendEvent(
		"response.content_part.added",
		openaifmt.BuildResponsesContentPartAddedPayload(
			s.responseID,
			s.ensureMessageItemID(),
			s.messageOutputIndex(),
			0,
			map[string]any{"type": "output_text", "text": ""},
		),
	)
	s.messagePartAdded = true
}

func (s *responsesStreamRuntime) emitTextDelta(content string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	s.ensureMessageContentPartAdded()
	s.visibleText.WriteString(content)
	s.sendEvent("response.output_text.delta", openaifmt.BuildResponsesTextDeltaPayload(s.responseID, content))
}

func (s *responsesStreamRuntime) closeMessageItem() {
	if !s.messageAdded {
		return
	}
	itemID := s.ensureMessageItemID()
	text := s.visibleText.String()
	if s.messagePartAdded {
		s.sendEvent(
			"response.content_part.done",
			openaifmt.BuildResponsesContentPartDonePayload(
				s.responseID,
				itemID,
				s.messageOutputIndex(),
				0,
				map[string]any{"type": "output_text", "text": text},
			),
		)
		s.messagePartAdded = false
	}
	item := map[string]any{
		"id":     itemID,
		"type":   "message",
		"role":   "assistant",
		"status": "completed",
		"content": []map[string]any{
			{
				"type": "output_text",
				"text": text,
			},
		},
	}
	s.sendEvent(
		"response.output_item.done",
		openaifmt.BuildResponsesOutputItemDonePayload(s.responseID, itemID, s.messageOutputIndex(), item),
	)
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

func (s *responsesStreamRuntime) functionOutputIndex(callIndex int) int {
	return s.functionOutputBaseIndex() + callIndex
}

func (s *responsesStreamRuntime) ensureFunctionItemAdded(callIndex int, name string) {
	if strings.TrimSpace(name) != "" {
		s.functionNames[callIndex] = strings.TrimSpace(name)
	}
	if s.functionAdded[callIndex] {
		return
	}
	fnName := strings.TrimSpace(s.functionNames[callIndex])
	if fnName == "" {
		return
	}
	outputIndex := s.functionOutputIndex(callIndex)
	itemID := s.ensureFunctionItemID(outputIndex)
	callID := s.ensureToolCallID(callIndex)
	item := map[string]any{
		"id":        itemID,
		"type":      "function_call",
		"call_id":   callID,
		"name":      fnName,
		"arguments": "{}",
		"status":    "in_progress",
	}
	s.sendEvent(
		"response.output_item.added",
		openaifmt.BuildResponsesOutputItemAddedPayload(s.responseID, itemID, outputIndex, item),
	)
	s.functionAdded[callIndex] = true
	s.toolCallsEmitted = true
}

func (s *responsesStreamRuntime) emitFunctionCallDeltaEvents(deltas []toolCallDelta) {
	for _, d := range deltas {
		s.ensureFunctionItemAdded(d.Index, d.Name)
		if strings.TrimSpace(d.Arguments) == "" {
			continue
		}
		outputIndex := s.functionOutputIndex(d.Index)
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
		s.ensureFunctionItemAdded(idx, tc.Name)

		outputIndex := base + idx
		if s.functionDone[outputIndex] {
			continue
		}
		itemID := s.ensureFunctionItemID(outputIndex)
		callID := s.ensureToolCallID(idx)
		argsBytes, _ := json.Marshal(tc.Input)
		args := string(argsBytes)
		s.sendEvent(
			"response.function_call_arguments.done",
			openaifmt.BuildResponsesFunctionCallArgumentsDonePayload(s.responseID, itemID, outputIndex, callID, tc.Name, args),
		)
		item := map[string]any{
			"id":        itemID,
			"type":      "function_call",
			"call_id":   callID,
			"name":      tc.Name,
			"arguments": args,
			"status":    "completed",
		}
		s.sendEvent(
			"response.output_item.done",
			openaifmt.BuildResponsesOutputItemDonePayload(s.responseID, itemID, outputIndex, item),
		)
		s.functionDone[outputIndex] = true
		s.toolCallsDoneEmitted = true
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
		if m["type"] != "function_call" {
			continue
		}
		if functionIdx < len(ordered) {
			m["call_id"] = ordered[functionIdx]
			functionIdx++
		}
	}
}
