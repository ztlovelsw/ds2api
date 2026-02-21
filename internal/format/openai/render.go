package openai

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"ds2api/internal/util"
)

func BuildChatCompletion(completionID, model, finalPrompt, finalThinking, finalText string, toolNames []string) map[string]any {
	detected := util.ParseToolCalls(finalText, toolNames)
	finishReason := "stop"
	messageObj := map[string]any{"role": "assistant", "content": finalText}
	if strings.TrimSpace(finalThinking) != "" {
		messageObj["reasoning_content"] = finalThinking
	}
	if len(detected) > 0 {
		finishReason = "tool_calls"
		messageObj["tool_calls"] = util.FormatOpenAIToolCalls(detected)
		messageObj["content"] = nil
	}
	promptTokens := util.EstimateTokens(finalPrompt)
	reasoningTokens := util.EstimateTokens(finalThinking)
	completionTokens := util.EstimateTokens(finalText)

	return map[string]any{
		"id":      completionID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{{"index": 0, "message": messageObj, "finish_reason": finishReason}},
		"usage": map[string]any{
			"prompt_tokens":     promptTokens,
			"completion_tokens": reasoningTokens + completionTokens,
			"total_tokens":      promptTokens + reasoningTokens + completionTokens,
			"completion_tokens_details": map[string]any{
				"reasoning_tokens": reasoningTokens,
			},
		},
	}
}

func BuildResponseObject(responseID, model, finalPrompt, finalThinking, finalText string, toolNames []string) map[string]any {
	// Align responses tool-call semantics with chat/completions:
	// mixed prose + tool_call payloads should still be interpreted as tool calls.
	detected := util.ParseToolCalls(finalText, toolNames)
	if len(detected) == 0 && strings.TrimSpace(finalThinking) != "" {
		detected = util.ParseToolCalls(finalThinking, toolNames)
	}
	exposedOutputText := finalText
	output := make([]any, 0, 2)
	if len(detected) > 0 {
		exposedOutputText = ""
		if strings.TrimSpace(finalThinking) != "" {
			output = append(output, map[string]any{
				"type": "reasoning",
				"text": finalThinking,
			})
		}
		formatted := util.FormatOpenAIToolCalls(detected)
		output = append(output, toResponsesFunctionCallItems(formatted)...)
		output = append(output, map[string]any{
			"type":       "tool_calls",
			"tool_calls": formatted,
		})
	} else {
		content := make([]any, 0, 2)
		if finalThinking != "" {
			content = append([]any{map[string]any{
				"type": "reasoning",
				"text": finalThinking,
			}}, content...)
		}
		if strings.TrimSpace(finalText) != "" {
			content = append(content, map[string]any{
				"type": "output_text",
				"text": finalText,
			})
		}
		if strings.TrimSpace(finalText) == "" && strings.TrimSpace(finalThinking) != "" {
			exposedOutputText = finalThinking
		}
		output = append(output, map[string]any{
			"type":    "message",
			"id":      "msg_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
			"role":    "assistant",
			"content": content,
		})
	}
	promptTokens := util.EstimateTokens(finalPrompt)
	reasoningTokens := util.EstimateTokens(finalThinking)
	completionTokens := util.EstimateTokens(finalText)
	return map[string]any{
		"id":          responseID,
		"type":        "response",
		"object":      "response",
		"created_at":  time.Now().Unix(),
		"status":      "completed",
		"model":       model,
		"output":      output,
		"output_text": exposedOutputText,
		"usage": map[string]any{
			"input_tokens":  promptTokens,
			"output_tokens": reasoningTokens + completionTokens,
			"total_tokens":  promptTokens + reasoningTokens + completionTokens,
		},
	}
}

func toResponsesFunctionCallItems(toolCalls []map[string]any) []any {
	if len(toolCalls) == 0 {
		return nil
	}
	out := make([]any, 0, len(toolCalls))
	for _, tc := range toolCalls {
		callID, _ := tc["id"].(string)
		if strings.TrimSpace(callID) == "" {
			callID = "call_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		}
		name := ""
		args := "{}"
		if fn, ok := tc["function"].(map[string]any); ok {
			if n, _ := fn["name"].(string); strings.TrimSpace(n) != "" {
				name = n
			}
			if a, _ := fn["arguments"].(string); strings.TrimSpace(a) != "" {
				args = a
			}
		}
		out = append(out, map[string]any{
			"id":        "fc_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
			"type":      "function_call",
			"call_id":   callID,
			"name":      name,
			"arguments": normalizeJSONString(args),
			"status":    "completed",
		})
	}
	return out
}

func normalizeJSONString(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "{}"
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return raw
	}
	b, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return string(b)
}

func BuildChatStreamDeltaChoice(index int, delta map[string]any) map[string]any {
	return map[string]any{
		"delta": delta,
		"index": index,
	}
}

func BuildChatStreamFinishChoice(index int, finishReason string) map[string]any {
	return map[string]any{
		"delta":         map[string]any{},
		"index":         index,
		"finish_reason": finishReason,
	}
}

func BuildChatStreamChunk(completionID string, created int64, model string, choices []map[string]any, usage map[string]any) map[string]any {
	out := map[string]any{
		"id":      completionID,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": choices,
	}
	if len(usage) > 0 {
		out["usage"] = usage
	}
	return out
}

func BuildChatUsage(finalPrompt, finalThinking, finalText string) map[string]any {
	promptTokens := util.EstimateTokens(finalPrompt)
	reasoningTokens := util.EstimateTokens(finalThinking)
	completionTokens := util.EstimateTokens(finalText)
	return map[string]any{
		"prompt_tokens":     promptTokens,
		"completion_tokens": reasoningTokens + completionTokens,
		"total_tokens":      promptTokens + reasoningTokens + completionTokens,
		"completion_tokens_details": map[string]any{
			"reasoning_tokens": reasoningTokens,
		},
	}
}

func BuildResponsesCreatedPayload(responseID, model string) map[string]any {
	return map[string]any{
		"type":        "response.created",
		"id":          responseID,
		"response_id": responseID,
		"object":      "response",
		"model":       model,
		"status":      "in_progress",
	}
}

func BuildResponsesTextDeltaPayload(responseID, delta string) map[string]any {
	return map[string]any{
		"type":        "response.output_text.delta",
		"id":          responseID,
		"response_id": responseID,
		"delta":       delta,
	}
}

func BuildResponsesReasoningDeltaPayload(responseID, delta string) map[string]any {
	return map[string]any{
		"type":        "response.reasoning.delta",
		"id":          responseID,
		"response_id": responseID,
		"delta":       delta,
	}
}

func BuildResponsesReasoningTextDeltaPayload(responseID, itemID string, outputIndex, contentIndex int, delta string) map[string]any {
	return map[string]any{
		"type":          "response.reasoning_text.delta",
		"id":            responseID,
		"response_id":   responseID,
		"item_id":       itemID,
		"output_index":  outputIndex,
		"content_index": contentIndex,
		"delta":         delta,
	}
}

func BuildResponsesReasoningTextDonePayload(responseID, itemID string, outputIndex, contentIndex int, text string) map[string]any {
	return map[string]any{
		"type":          "response.reasoning_text.done",
		"id":            responseID,
		"response_id":   responseID,
		"item_id":       itemID,
		"output_index":  outputIndex,
		"content_index": contentIndex,
		"text":          text,
	}
}

func BuildResponsesToolCallDeltaPayload(responseID string, toolCalls []map[string]any) map[string]any {
	return map[string]any{
		"type":        "response.output_tool_call.delta",
		"id":          responseID,
		"response_id": responseID,
		"tool_calls":  toolCalls,
	}
}

func BuildResponsesToolCallDonePayload(responseID string, toolCalls []map[string]any) map[string]any {
	return map[string]any{
		"type":        "response.output_tool_call.done",
		"id":          responseID,
		"response_id": responseID,
		"tool_calls":  toolCalls,
	}
}

func BuildResponsesFunctionCallArgumentsDeltaPayload(responseID, itemID string, outputIndex int, callID, delta string) map[string]any {
	return map[string]any{
		"type":         "response.function_call_arguments.delta",
		"id":           responseID,
		"response_id":  responseID,
		"item_id":      itemID,
		"output_index": outputIndex,
		"call_id":      callID,
		"delta":        delta,
	}
}

func BuildResponsesFunctionCallArgumentsDonePayload(responseID, itemID string, outputIndex int, callID, name, arguments string) map[string]any {
	return map[string]any{
		"type":         "response.function_call_arguments.done",
		"id":           responseID,
		"response_id":  responseID,
		"item_id":      itemID,
		"output_index": outputIndex,
		"call_id":      callID,
		"name":         name,
		"arguments":    normalizeJSONString(arguments),
	}
}

func BuildResponsesCompletedPayload(response map[string]any) map[string]any {
	responseID, _ := response["id"].(string)
	return map[string]any{
		"type":        "response.completed",
		"response_id": responseID,
		"response":    response,
	}
}
