package openai

import (
	"encoding/json"
	"sort"
	"strings"

	openaifmt "ds2api/internal/format/openai"
	"ds2api/internal/util"

	"github.com/google/uuid"
)

func (s *responsesStreamRuntime) allocateOutputIndex() int {
	idx := s.nextOutputID
	s.nextOutputID++
	return idx
}

func (s *responsesStreamRuntime) ensureMessageItemID() string {
	if strings.TrimSpace(s.messageItemID) != "" {
		return s.messageItemID
	}
	s.messageItemID = "msg_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	return s.messageItemID
}

func (s *responsesStreamRuntime) ensureMessageOutputIndex() int {
	if s.messageOutputID >= 0 {
		return s.messageOutputID
	}
	s.messageOutputID = s.allocateOutputIndex()
	return s.messageOutputID
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
		openaifmt.BuildResponsesOutputItemAddedPayload(s.responseID, itemID, s.ensureMessageOutputIndex(), item),
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
			s.ensureMessageOutputIndex(),
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
	s.sendEvent(
		"response.output_text.delta",
		openaifmt.BuildResponsesTextDeltaPayload(
			s.responseID,
			s.ensureMessageItemID(),
			s.ensureMessageOutputIndex(),
			0,
			content,
		),
	)
}

func (s *responsesStreamRuntime) closeMessageItem() {
	if !s.messageAdded {
		return
	}
	itemID := s.ensureMessageItemID()
	outputIndex := s.ensureMessageOutputIndex()
	text := s.visibleText.String()
	if s.messagePartAdded {
		s.sendEvent(
			"response.content_part.done",
			openaifmt.BuildResponsesContentPartDonePayload(
				s.responseID,
				itemID,
				outputIndex,
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
		openaifmt.BuildResponsesOutputItemDonePayload(s.responseID, itemID, outputIndex, item),
	)
}

func (s *responsesStreamRuntime) ensureFunctionItemID(callIndex int) string {
	if id, ok := s.functionItemIDs[callIndex]; ok && strings.TrimSpace(id) != "" {
		return id
	}
	id := "fc_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	s.functionItemIDs[callIndex] = id
	return id
}

func (s *responsesStreamRuntime) ensureToolCallID(callIndex int) string {
	if id, ok := s.streamToolCallIDs[callIndex]; ok && strings.TrimSpace(id) != "" {
		return id
	}
	id := "call_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	s.streamToolCallIDs[callIndex] = id
	return id
}

func (s *responsesStreamRuntime) ensureFunctionOutputIndex(callIndex int) int {
	if idx, ok := s.functionOutputIDs[callIndex]; ok {
		return idx
	}
	idx := s.allocateOutputIndex()
	s.functionOutputIDs[callIndex] = idx
	return idx
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
	outputIndex := s.ensureFunctionOutputIndex(callIndex)
	itemID := s.ensureFunctionItemID(callIndex)
	callID := s.ensureToolCallID(callIndex)
	item := map[string]any{
		"id":        itemID,
		"type":      "function_call",
		"call_id":   callID,
		"name":      fnName,
		"arguments": "",
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
		s.functionArgs[d.Index] += d.Arguments
		outputIndex := s.ensureFunctionOutputIndex(d.Index)
		itemID := s.ensureFunctionItemID(d.Index)
		callID := s.ensureToolCallID(d.Index)
		s.sendEvent(
			"response.function_call_arguments.delta",
			openaifmt.BuildResponsesFunctionCallArgumentsDeltaPayload(s.responseID, itemID, outputIndex, callID, d.Arguments),
		)
	}
}

func (s *responsesStreamRuntime) emitFunctionCallDoneEvents(calls []util.ParsedToolCall) {
	for idx, tc := range calls {
		if strings.TrimSpace(tc.Name) == "" {
			continue
		}
		s.ensureFunctionItemAdded(idx, tc.Name)
		if s.functionDone[idx] {
			continue
		}
		outputIndex := s.ensureFunctionOutputIndex(idx)
		itemID := s.ensureFunctionItemID(idx)
		callID := s.ensureToolCallID(idx)
		argsBytes, _ := json.Marshal(tc.Input)
		args := string(argsBytes)
		s.functionArgs[idx] = args
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
		s.functionDone[idx] = true
		s.toolCallsDoneEmitted = true
	}
}

func (s *responsesStreamRuntime) closeIncompleteFunctionItems() {
	if len(s.functionAdded) == 0 {
		return
	}
	indices := make([]int, 0, len(s.functionAdded))
	for idx, added := range s.functionAdded {
		if !added || s.functionDone[idx] {
			continue
		}
		indices = append(indices, idx)
	}
	if len(indices) == 0 {
		return
	}
	sort.Ints(indices)
	for _, idx := range indices {
		name := strings.TrimSpace(s.functionNames[idx])
		if name == "" {
			continue
		}
		args := strings.TrimSpace(s.functionArgs[idx])
		if args == "" {
			args = "{}"
		}
		outputIndex := s.ensureFunctionOutputIndex(idx)
		itemID := s.ensureFunctionItemID(idx)
		callID := s.ensureToolCallID(idx)
		s.sendEvent(
			"response.function_call_arguments.done",
			openaifmt.BuildResponsesFunctionCallArgumentsDonePayload(s.responseID, itemID, outputIndex, callID, name, args),
		)
		item := map[string]any{
			"id":        itemID,
			"type":      "function_call",
			"call_id":   callID,
			"name":      name,
			"arguments": args,
			"status":    "completed",
		}
		s.sendEvent(
			"response.output_item.done",
			openaifmt.BuildResponsesOutputItemDonePayload(s.responseID, itemID, outputIndex, item),
		)
		s.functionDone[idx] = true
		s.toolCallsDoneEmitted = true
	}
}

func (s *responsesStreamRuntime) buildCompletedResponseObject(finalThinking, finalText string, calls []util.ParsedToolCall) map[string]any {
	type indexedItem struct {
		index int
		item  map[string]any
	}
	indexed := make([]indexedItem, 0, len(calls)+1)

	if s.messageAdded {
		text := s.visibleText.String()
		indexed = append(indexed, indexedItem{
			index: s.ensureMessageOutputIndex(),
			item: map[string]any{
				"id":     s.ensureMessageItemID(),
				"type":   "message",
				"role":   "assistant",
				"status": "completed",
				"content": []map[string]any{
					{
						"type": "output_text",
						"text": text,
					},
				},
			},
		})
	} else if len(calls) == 0 {
		content := make([]map[string]any, 0, 2)
		if strings.TrimSpace(finalThinking) != "" {
			content = append(content, map[string]any{
				"type": "reasoning",
				"text": finalThinking,
			})
		}
		if strings.TrimSpace(finalText) != "" {
			content = append(content, map[string]any{
				"type": "output_text",
				"text": finalText,
			})
		}
		if len(content) > 0 {
			indexed = append(indexed, indexedItem{
				index: s.ensureMessageOutputIndex(),
				item: map[string]any{
					"id":      s.ensureMessageItemID(),
					"type":    "message",
					"role":    "assistant",
					"status":  "completed",
					"content": content,
				},
			})
		}
	}

	for idx, tc := range calls {
		if strings.TrimSpace(tc.Name) == "" {
			continue
		}
		argsBytes, _ := json.Marshal(tc.Input)
		indexed = append(indexed, indexedItem{
			index: s.ensureFunctionOutputIndex(idx),
			item: map[string]any{
				"id":        s.ensureFunctionItemID(idx),
				"type":      "function_call",
				"call_id":   s.ensureToolCallID(idx),
				"name":      tc.Name,
				"arguments": string(argsBytes),
				"status":    "completed",
			},
		})
	}

	sort.SliceStable(indexed, func(i, j int) bool {
		return indexed[i].index < indexed[j].index
	})
	output := make([]any, 0, len(indexed))
	for _, it := range indexed {
		output = append(output, it.item)
	}

	outputText := s.visibleText.String()
	if strings.TrimSpace(outputText) == "" && len(calls) == 0 {
		if strings.TrimSpace(finalText) != "" {
			outputText = finalText
		} else if strings.TrimSpace(finalThinking) != "" {
			outputText = finalThinking
		}
	}

	return openaifmt.BuildResponseObjectFromItems(
		s.responseID,
		s.model,
		s.finalPrompt,
		finalThinking,
		finalText,
		output,
		outputText,
	)
}
