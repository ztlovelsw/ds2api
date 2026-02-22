package openai

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
