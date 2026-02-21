package openai

import (
	"encoding/json"
	"testing"
)

func TestBuildResponseObjectToolCallsFollowChatShape(t *testing.T) {
	obj := BuildResponseObject(
		"resp_test",
		"gpt-4o",
		"prompt",
		"",
		`{"tool_calls":[{"name":"search","input":{"q":"golang"}}]}`,
		[]string{"search"},
	)

	outputText, _ := obj["output_text"].(string)
	if outputText != "" {
		t.Fatalf("expected output_text to be hidden for tool calls, got %q", outputText)
	}

	output, _ := obj["output"].([]any)
	if len(output) != 2 {
		t.Fatalf("expected function_call + tool_calls wrapper, got %#v", obj["output"])
	}

	first, _ := output[0].(map[string]any)
	if first["type"] != "function_call" {
		t.Fatalf("expected first output item type function_call, got %#v", first["type"])
	}
	if first["call_id"] == "" {
		t.Fatalf("expected function_call item to have call_id, got %#v", first)
	}
	second, _ := output[1].(map[string]any)
	if second["type"] != "tool_calls" {
		t.Fatalf("expected second output item type tool_calls, got %#v", second["type"])
	}
	var toolCalls []map[string]any
	switch v := second["tool_calls"].(type) {
	case []map[string]any:
		toolCalls = v
	case []any:
		toolCalls = make([]map[string]any, 0, len(v))
		for _, item := range v {
			m, _ := item.(map[string]any)
			if m != nil {
				toolCalls = append(toolCalls, m)
			}
		}
	}
	if len(toolCalls) != 1 {
		t.Fatalf("expected one tool call, got %#v", second["tool_calls"])
	}
	tc := toolCalls[0]
	if tc["type"] != "function" || tc["id"] == "" {
		t.Fatalf("unexpected tool call shape: %#v", tc)
	}
	fn, _ := tc["function"].(map[string]any)
	if fn["name"] != "search" {
		t.Fatalf("unexpected function name: %#v", fn["name"])
	}
	argsRaw, _ := fn["arguments"].(string)
	var args map[string]any
	if err := json.Unmarshal([]byte(argsRaw), &args); err != nil {
		t.Fatalf("arguments should be valid json string, got=%q err=%v", argsRaw, err)
	}
	if args["q"] != "golang" {
		t.Fatalf("unexpected arguments: %#v", args)
	}
}

func TestBuildResponseObjectTreatsMixedProseToolPayloadAsToolCall(t *testing.T) {
	obj := BuildResponseObject(
		"resp_test",
		"gpt-4o",
		"prompt",
		"",
		`示例格式：{"tool_calls":[{"name":"search","input":{"q":"golang"}}]}，但这条是普通回答。`,
		[]string{"search"},
	)

	outputText, _ := obj["output_text"].(string)
	if outputText != "" {
		t.Fatalf("expected output_text hidden once tool calls are detected, got %q", outputText)
	}

	output, _ := obj["output"].([]any)
	if len(output) != 2 {
		t.Fatalf("expected function_call + tool_calls wrapper, got %#v", obj["output"])
	}
	first, _ := output[0].(map[string]any)
	if first["type"] != "function_call" {
		t.Fatalf("expected first output type function_call, got %#v", first["type"])
	}
}

func TestBuildResponseObjectFencedToolPayloadRemainsText(t *testing.T) {
	obj := BuildResponseObject(
		"resp_test",
		"gpt-4o",
		"prompt",
		"",
		"```json\n{\"tool_calls\":[{\"name\":\"search\",\"input\":{\"q\":\"golang\"}}]}\n```",
		[]string{"search"},
	)

	outputText, _ := obj["output_text"].(string)
	if outputText == "" {
		t.Fatalf("expected output_text preserved for fenced example")
	}
	output, _ := obj["output"].([]any)
	if len(output) != 1 {
		t.Fatalf("expected one message output item, got %#v", obj["output"])
	}
	first, _ := output[0].(map[string]any)
	if first["type"] != "message" {
		t.Fatalf("expected message output type, got %#v", first["type"])
	}
}

func TestBuildResponseObjectReasoningOnlyFallsBackToOutputText(t *testing.T) {
	obj := BuildResponseObject(
		"resp_test",
		"gpt-4o",
		"prompt",
		"internal thinking content",
		"",
		nil,
	)

	outputText, _ := obj["output_text"].(string)
	if outputText == "" {
		t.Fatalf("expected output_text fallback from reasoning when final text is empty")
	}

	output, _ := obj["output"].([]any)
	if len(output) != 1 {
		t.Fatalf("expected one output item, got %#v", obj["output"])
	}
	first, _ := output[0].(map[string]any)
	if first["type"] != "message" {
		t.Fatalf("expected output type message, got %#v", first["type"])
	}
	content, _ := first["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("expected reasoning content, got %#v", first["content"])
	}
	block0, _ := content[0].(map[string]any)
	if block0["type"] != "reasoning" {
		t.Fatalf("expected first content block reasoning, got %#v", block0["type"])
	}
}

func TestBuildResponseObjectDetectsToolCallFromThinkingChannel(t *testing.T) {
	obj := BuildResponseObject(
		"resp_test",
		"gpt-4o",
		"prompt",
		`{"tool_calls":[{"name":"search","input":{"q":"from-thinking"}}]}`,
		"",
		[]string{"search"},
	)

	output, _ := obj["output"].([]any)
	if len(output) != 3 {
		t.Fatalf("expected reasoning + function_call + tool_calls outputs, got %#v", obj["output"])
	}
	first, _ := output[0].(map[string]any)
	if first["type"] != "reasoning" {
		t.Fatalf("expected first output reasoning, got %#v", first["type"])
	}
	second, _ := output[1].(map[string]any)
	if second["type"] != "function_call" {
		t.Fatalf("expected second output function_call, got %#v", second["type"])
	}
	third, _ := output[2].(map[string]any)
	if third["type"] != "tool_calls" {
		t.Fatalf("expected third output tool_calls, got %#v", third["type"])
	}
}
