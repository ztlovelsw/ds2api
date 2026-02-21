package openai

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleResponsesStreamToolCallsHideRawOutputTextInCompleted(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	rec := httptest.NewRecorder()

	sseLine := func(v string) string {
		b, _ := json.Marshal(map[string]any{
			"p": "response/content",
			"v": v,
		})
		return "data: " + string(b) + "\n"
	}

	rawToolJSON := `{"tool_calls":[{"name":"read_file","input":{"path":"README.MD"}}]}`
	streamBody := sseLine(rawToolJSON) + "data: [DONE]\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}

	h.handleResponsesStream(rec, req, resp, "owner-a", "resp_test", "deepseek-chat", "prompt", false, false, []string{"read_file"})

	completed, ok := extractSSEEventPayload(rec.Body.String(), "response.completed")
	if !ok {
		t.Fatalf("expected response.completed event, body=%s", rec.Body.String())
	}
	responseObj, _ := completed["response"].(map[string]any)
	outputText, _ := responseObj["output_text"].(string)
	if outputText != "" {
		t.Fatalf("expected empty output_text for tool_calls response, got output_text=%q", outputText)
	}
	output, _ := responseObj["output"].([]any)
	if len(output) == 0 {
		t.Fatalf("expected structured output entries, got %#v", responseObj["output"])
	}
	var firstToolWrapper map[string]any
	hasFunctionCall := false
	for _, item := range output {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		if m["type"] == "function_call" {
			hasFunctionCall = true
		}
		if m["type"] == "tool_calls" && firstToolWrapper == nil {
			firstToolWrapper = m
		}
	}
	if !hasFunctionCall {
		t.Fatalf("expected at least one function_call item for responses compatibility, got %#v", responseObj["output"])
	}
	if firstToolWrapper == nil {
		t.Fatalf("expected a tool_calls wrapper item, got %#v", responseObj["output"])
	}
	toolCalls, _ := firstToolWrapper["tool_calls"].([]any)
	if len(toolCalls) == 0 {
		t.Fatalf("expected at least one tool_call in output, got %#v", firstToolWrapper["tool_calls"])
	}
	call0, _ := toolCalls[0].(map[string]any)
	if call0["type"] != "function" {
		t.Fatalf("unexpected tool call type: %#v", call0["type"])
	}
	fn, _ := call0["function"].(map[string]any)
	if fn["name"] != "read_file" {
		t.Fatalf("unexpected tool call name: %#v", fn["name"])
	}
	if strings.Contains(outputText, `"tool_calls"`) {
		t.Fatalf("raw tool_calls JSON leaked in output_text: %q", outputText)
	}
}

func TestHandleResponsesStreamIncompleteTailNotDuplicatedInCompletedOutputText(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	rec := httptest.NewRecorder()

	sseLine := func(v string) string {
		b, _ := json.Marshal(map[string]any{
			"p": "response/content",
			"v": v,
		})
		return "data: " + string(b) + "\n"
	}

	tail := `{"tool_calls":[{"name":"read_file","input":`
	streamBody := sseLine("Before ") + sseLine(tail) + "data: [DONE]\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}

	h.handleResponsesStream(rec, req, resp, "owner-a", "resp_test", "deepseek-chat", "prompt", false, false, []string{"read_file"})

	completed, ok := extractSSEEventPayload(rec.Body.String(), "response.completed")
	if !ok {
		t.Fatalf("expected response.completed event, body=%s", rec.Body.String())
	}
	responseObj, _ := completed["response"].(map[string]any)
	outputText, _ := responseObj["output_text"].(string)
	if strings.Count(outputText, tail) > 1 {
		t.Fatalf("expected incomplete tail not to be duplicated, got output_text=%q", outputText)
	}
}

func TestHandleResponsesStreamEmitsReasoningCompatEvents(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	rec := httptest.NewRecorder()

	b, _ := json.Marshal(map[string]any{
		"p": "response/thinking_content",
		"v": "thought",
	})
	streamBody := "data: " + string(b) + "\n" + "data: [DONE]\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}

	h.handleResponsesStream(rec, req, resp, "owner-a", "resp_test", "deepseek-reasoner", "prompt", true, false, nil)

	body := rec.Body.String()
	if !strings.Contains(body, "event: response.reasoning.delta") {
		t.Fatalf("expected response.reasoning.delta event, body=%s", body)
	}
	if !strings.Contains(body, "event: response.reasoning_text.delta") {
		t.Fatalf("expected response.reasoning_text.delta compatibility event, body=%s", body)
	}
	if !strings.Contains(body, "event: response.reasoning_text.done") {
		t.Fatalf("expected response.reasoning_text.done compatibility event, body=%s", body)
	}
}

func TestHandleResponsesStreamEmitsFunctionCallCompatEvents(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	rec := httptest.NewRecorder()

	sseLine := func(v string) string {
		b, _ := json.Marshal(map[string]any{
			"p": "response/content",
			"v": v,
		})
		return "data: " + string(b) + "\n"
	}

	streamBody := sseLine(`{"tool_calls":[{"name":"read_file","input":{"path":"README.MD"}}]}`) + "data: [DONE]\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}

	h.handleResponsesStream(rec, req, resp, "owner-a", "resp_test", "deepseek-chat", "prompt", false, false, []string{"read_file"})
	body := rec.Body.String()
	if !strings.Contains(body, "event: response.function_call_arguments.delta") {
		t.Fatalf("expected response.function_call_arguments.delta compatibility event, body=%s", body)
	}
	if !strings.Contains(body, "event: response.function_call_arguments.done") {
		t.Fatalf("expected response.function_call_arguments.done compatibility event, body=%s", body)
	}
	donePayload, ok := extractSSEEventPayload(body, "response.function_call_arguments.done")
	if !ok {
		t.Fatalf("expected to parse response.function_call_arguments.done payload, body=%s", body)
	}
	if strings.TrimSpace(asString(donePayload["call_id"])) == "" {
		t.Fatalf("expected call_id in response.function_call_arguments.done payload, payload=%#v", donePayload)
	}
	if strings.TrimSpace(asString(donePayload["response_id"])) == "" {
		t.Fatalf("expected response_id in response.function_call_arguments.done payload, payload=%#v", donePayload)
	}
	doneCallID := strings.TrimSpace(asString(donePayload["call_id"]))
	if doneCallID == "" {
		t.Fatalf("expected non-empty call_id in done payload, payload=%#v", donePayload)
	}
	completed, ok := extractSSEEventPayload(body, "response.completed")
	if !ok {
		t.Fatalf("expected response.completed payload, body=%s", body)
	}
	responseObj, _ := completed["response"].(map[string]any)
	output, _ := responseObj["output"].([]any)
	if len(output) == 0 {
		t.Fatalf("expected non-empty output in response.completed, response=%#v", responseObj)
	}
	var completedCallID string
	for _, item := range output {
		m, _ := item.(map[string]any)
		if m == nil || m["type"] != "function_call" {
			continue
		}
		completedCallID = strings.TrimSpace(asString(m["call_id"]))
		if completedCallID != "" {
			break
		}
	}
	if completedCallID == "" {
		t.Fatalf("expected function_call.call_id in completed output, output=%#v", output)
	}
	if completedCallID != doneCallID {
		t.Fatalf("expected completed call_id to match stream done call_id, done=%q completed=%q", doneCallID, completedCallID)
	}
}

func TestHandleResponsesStreamDetectsToolCallsFromThinkingChannel(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	rec := httptest.NewRecorder()

	sseLine := func(path, v string) string {
		b, _ := json.Marshal(map[string]any{
			"p": path,
			"v": v,
		})
		return "data: " + string(b) + "\n"
	}

	streamBody := sseLine("response/thinking_content", `{"tool_calls":[{"name":"read_file","input":{"path":"README.MD"}}]}`) + "data: [DONE]\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}

	h.handleResponsesStream(rec, req, resp, "owner-a", "resp_test", "deepseek-reasoner", "prompt", true, false, []string{"read_file"})

	body := rec.Body.String()
	if !strings.Contains(body, "event: response.reasoning_text.delta") {
		t.Fatalf("expected response.reasoning_text.delta event, body=%s", body)
	}
	if !strings.Contains(body, "event: response.function_call_arguments.done") {
		t.Fatalf("expected response.function_call_arguments.done event from thinking channel, body=%s", body)
	}
	if !strings.Contains(body, "event: response.output_tool_call.done") {
		t.Fatalf("expected response.output_tool_call.done event from thinking channel, body=%s", body)
	}
}

func TestHandleResponsesStreamMultiToolCallKeepsNameAndCallIDAligned(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	rec := httptest.NewRecorder()

	sseLine := func(v string) string {
		b, _ := json.Marshal(map[string]any{
			"p": "response/content",
			"v": v,
		})
		return "data: " + string(b) + "\n"
	}

	streamBody := sseLine(`{"tool_calls":[{"name":"search_web","input":{"query":"latest ai news"}},`) +
		sseLine(`{"name":"eval_javascript","input":{"code":"1+1"}}]}`) +
		"data: [DONE]\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}

	h.handleResponsesStream(rec, req, resp, "owner-a", "resp_test", "deepseek-chat", "prompt", false, false, []string{"search_web", "eval_javascript"})

	body := rec.Body.String()
	if !strings.Contains(body, "event: response.output_tool_call.done") {
		t.Fatalf("expected response.output_tool_call.done event, body=%s", body)
	}
	donePayloads := extractAllSSEEventPayloads(body, "response.function_call_arguments.done")
	if len(donePayloads) != 2 {
		t.Fatalf("expected two response.function_call_arguments.done events, got %d body=%s", len(donePayloads), body)
	}

	seenNames := map[string]string{}
	for _, payload := range donePayloads {
		name := strings.TrimSpace(asString(payload["name"]))
		callID := strings.TrimSpace(asString(payload["call_id"]))
		args := strings.TrimSpace(asString(payload["arguments"]))
		if callID == "" {
			t.Fatalf("expected non-empty call_id in done payload: %#v", payload)
		}
		if strings.Contains(args, `}{"`) {
			t.Fatalf("unexpected concatenated arguments in done payload: %#v", payload)
		}
		if name == "search_webeval_javascript" {
			t.Fatalf("unexpected merged tool name in done payload: %#v", payload)
		}
		if name != "search_web" && name != "eval_javascript" {
			t.Fatalf("unexpected tool name in done payload: %#v", payload)
		}
		seenNames[name] = callID
	}
	if seenNames["search_web"] == "" || seenNames["eval_javascript"] == "" {
		t.Fatalf("expected done events for both tools, got %#v", seenNames)
	}
	if seenNames["search_web"] == seenNames["eval_javascript"] {
		t.Fatalf("expected distinct call_id per tool, got %#v", seenNames)
	}

	completed, ok := extractSSEEventPayload(body, "response.completed")
	if !ok {
		t.Fatalf("expected response.completed event, body=%s", body)
	}
	responseObj, _ := completed["response"].(map[string]any)
	output, _ := responseObj["output"].([]any)
	functionCallIDs := map[string]string{}
	for _, item := range output {
		m, _ := item.(map[string]any)
		if m == nil || m["type"] != "function_call" {
			continue
		}
		name := strings.TrimSpace(asString(m["name"]))
		callID := strings.TrimSpace(asString(m["call_id"]))
		if name != "" && callID != "" {
			functionCallIDs[name] = callID
		}
	}
	if functionCallIDs["search_web"] != seenNames["search_web"] {
		t.Fatalf("search_web call_id mismatch between done and completed: done=%q completed=%q", seenNames["search_web"], functionCallIDs["search_web"])
	}
	if functionCallIDs["eval_javascript"] != seenNames["eval_javascript"] {
		t.Fatalf("eval_javascript call_id mismatch between done and completed: done=%q completed=%q", seenNames["eval_javascript"], functionCallIDs["eval_javascript"])
	}
}

func TestHandleResponsesStreamMultiToolCallFromThinkingChannel(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	rec := httptest.NewRecorder()

	sseLine := func(path, v string) string {
		b, _ := json.Marshal(map[string]any{
			"p": path,
			"v": v,
		})
		return "data: " + string(b) + "\n"
	}

	streamBody := sseLine("response/thinking_content", `{"tool_calls":[{"name":"search_web","input":{"query":"latest ai news"}},`) +
		sseLine("response/thinking_content", `{"name":"eval_javascript","input":{"code":"1+1"}}]}`) +
		"data: [DONE]\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}

	h.handleResponsesStream(rec, req, resp, "owner-a", "resp_test", "deepseek-reasoner", "prompt", true, false, []string{"search_web", "eval_javascript"})

	body := rec.Body.String()
	if !strings.Contains(body, "event: response.reasoning_text.delta") {
		t.Fatalf("expected reasoning stream events, body=%s", body)
	}
	donePayloads := extractAllSSEEventPayloads(body, "response.function_call_arguments.done")
	if len(donePayloads) != 2 {
		t.Fatalf("expected two response.function_call_arguments.done events, got %d body=%s", len(donePayloads), body)
	}
	seen := map[string]bool{}
	for _, payload := range donePayloads {
		name := strings.TrimSpace(asString(payload["name"]))
		if name == "search_webeval_javascript" {
			t.Fatalf("unexpected merged tool name in thinking channel done payload: %#v", payload)
		}
		if name != "search_web" && name != "eval_javascript" {
			t.Fatalf("unexpected tool name in thinking channel done payload: %#v", payload)
		}
		args := strings.TrimSpace(asString(payload["arguments"]))
		if strings.Contains(args, `}{"`) {
			t.Fatalf("unexpected concatenated arguments in thinking channel done payload: %#v", payload)
		}
		seen[name] = true
	}
	if !seen["search_web"] || !seen["eval_javascript"] {
		t.Fatalf("expected both tools in thinking channel done events, got %#v", seen)
	}
}

func TestHandleResponsesStreamCompletedFollowsChatToolCallSemantics(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	rec := httptest.NewRecorder()

	sseLine := func(v string) string {
		b, _ := json.Marshal(map[string]any{
			"p": "response/content",
			"v": v,
		})
		return "data: " + string(b) + "\n"
	}

	streamBody := sseLine("我来调用工具\n") +
		sseLine(`{"tool_calls":[{"name":"read_file","input":{"path":"README.MD"}}]}`) +
		"data: [DONE]\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}

	h.handleResponsesStream(rec, req, resp, "owner-a", "resp_test", "deepseek-chat", "prompt", false, false, []string{"read_file"})

	completed, ok := extractSSEEventPayload(rec.Body.String(), "response.completed")
	if !ok {
		t.Fatalf("expected response.completed event, body=%s", rec.Body.String())
	}
	responseObj, _ := completed["response"].(map[string]any)
	output, _ := responseObj["output"].([]any)
	hasFunctionCall := false
	for _, item := range output {
		m, _ := item.(map[string]any)
		if m != nil && m["type"] == "function_call" {
			hasFunctionCall = true
			break
		}
	}
	if !hasFunctionCall {
		t.Fatalf("expected completed output to include function_call when mixed prose contains tool_calls payload, output=%#v", output)
	}
}

func extractSSEEventPayload(body, targetEvent string) (map[string]any, bool) {
	scanner := bufio.NewScanner(strings.NewReader(body))
	matched := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "event: ") {
			evt := strings.TrimSpace(strings.TrimPrefix(line, "event: "))
			matched = evt == targetEvent
			continue
		}
		if !matched || !strings.HasPrefix(line, "data: ") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		if raw == "" || raw == "[DONE]" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return nil, false
		}
		return payload, true
	}
	return nil, false
}

func extractAllSSEEventPayloads(body, targetEvent string) []map[string]any {
	scanner := bufio.NewScanner(strings.NewReader(body))
	matched := false
	out := make([]map[string]any, 0, 2)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "event: ") {
			evt := strings.TrimSpace(strings.TrimPrefix(line, "event: "))
			matched = evt == targetEvent
			continue
		}
		if !matched || !strings.HasPrefix(line, "data: ") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		if raw == "" || raw == "[DONE]" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			continue
		}
		out = append(out, payload)
	}
	return out
}
