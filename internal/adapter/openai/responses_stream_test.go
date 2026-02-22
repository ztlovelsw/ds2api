package openai

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ds2api/internal/util"
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

	h.handleResponsesStream(rec, req, resp, "owner-a", "resp_test", "deepseek-chat", "prompt", false, false, []string{"read_file"}, util.DefaultToolChoicePolicy(), "")

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
	hasFunctionCall := false
	hasLegacyWrapper := false
	for _, item := range output {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		if m["type"] == "function_call" {
			hasFunctionCall = true
		}
		if m["type"] == "tool_calls" {
			hasLegacyWrapper = true
		}
	}
	if !hasFunctionCall {
		t.Fatalf("expected function_call item, got %#v", responseObj["output"])
	}
	if hasLegacyWrapper {
		t.Fatalf("did not expect legacy tool_calls wrapper, got %#v", responseObj["output"])
	}
	if strings.Contains(outputText, `"tool_calls"`) {
		t.Fatalf("raw tool_calls JSON leaked in output_text: %q", outputText)
	}
}

func TestHandleResponsesStreamUsesOfficialOutputItemEvents(t *testing.T) {
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

	h.handleResponsesStream(rec, req, resp, "owner-a", "resp_test", "deepseek-chat", "prompt", false, false, []string{"read_file"}, util.DefaultToolChoicePolicy(), "")
	body := rec.Body.String()
	if !strings.Contains(body, "event: response.output_item.added") {
		t.Fatalf("expected response.output_item.added event, body=%s", body)
	}
	if !strings.Contains(body, "event: response.output_item.done") {
		t.Fatalf("expected response.output_item.done event, body=%s", body)
	}
	if !strings.Contains(body, "event: response.function_call_arguments.delta") {
		t.Fatalf("expected response.function_call_arguments.delta event, body=%s", body)
	}
	if !strings.Contains(body, "event: response.function_call_arguments.done") {
		t.Fatalf("expected response.function_call_arguments.done event, body=%s", body)
	}
	if strings.Contains(body, "event: response.output_tool_call.delta") || strings.Contains(body, "event: response.output_tool_call.done") {
		t.Fatalf("legacy response.output_tool_call.* event must not appear, body=%s", body)
	}

	donePayload, ok := extractSSEEventPayload(body, "response.function_call_arguments.done")
	if !ok {
		t.Fatalf("expected to parse response.function_call_arguments.done payload, body=%s", body)
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

func TestHandleResponsesStreamDoesNotEmitReasoningTextCompatEvents(t *testing.T) {
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

	h.handleResponsesStream(rec, req, resp, "owner-a", "resp_test", "deepseek-reasoner", "prompt", true, false, nil, util.DefaultToolChoicePolicy(), "")

	body := rec.Body.String()
	if !strings.Contains(body, "event: response.reasoning.delta") {
		t.Fatalf("expected response.reasoning.delta event, body=%s", body)
	}
	if strings.Contains(body, "event: response.reasoning_text.delta") || strings.Contains(body, "event: response.reasoning_text.done") {
		t.Fatalf("did not expect response.reasoning_text.* compatibility events, body=%s", body)
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

	h.handleResponsesStream(rec, req, resp, "owner-a", "resp_test", "deepseek-chat", "prompt", false, false, []string{"search_web", "eval_javascript"}, util.DefaultToolChoicePolicy(), "")

	body := rec.Body.String()
	donePayloads := extractAllSSEEventPayloads(body, "response.function_call_arguments.done")
	if len(donePayloads) != 2 {
		t.Fatalf("expected two response.function_call_arguments.done events, got %d body=%s", len(donePayloads), body)
	}
	seenNames := map[string]string{}
	for _, payload := range donePayloads {
		name := strings.TrimSpace(asString(payload["name"]))
		callID := strings.TrimSpace(asString(payload["call_id"]))
		if name != "search_web" && name != "eval_javascript" {
			t.Fatalf("unexpected tool name in done payload: %#v", payload)
		}
		if callID == "" {
			t.Fatalf("expected non-empty call_id in done payload: %#v", payload)
		}
		seenNames[name] = callID
	}
	if seenNames["search_web"] == seenNames["eval_javascript"] {
		t.Fatalf("expected distinct call_id per tool, got %#v", seenNames)
	}
}

func TestHandleResponsesStreamRequiredToolChoiceFailure(t *testing.T) {
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

	streamBody := sseLine("plain text only") + "data: [DONE]\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}

	policy := util.ToolChoicePolicy{
		Mode:    util.ToolChoiceRequired,
		Allowed: map[string]struct{}{"read_file": {}},
	}
	h.handleResponsesStream(rec, req, resp, "owner-a", "resp_test", "deepseek-chat", "prompt", false, false, []string{"read_file"}, policy, "")

	body := rec.Body.String()
	if !strings.Contains(body, "event: response.failed") {
		t.Fatalf("expected response.failed event for required tool_choice violation, body=%s", body)
	}
	if strings.Contains(body, "event: response.completed") {
		t.Fatalf("did not expect response.completed after failure, body=%s", body)
	}
}

func TestHandleResponsesStreamRejectsUnknownToolName(t *testing.T) {
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

	streamBody := sseLine(`{"tool_calls":[{"name":"not_in_schema","input":{"q":"go"}}]}`) + "data: [DONE]\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}

	h.handleResponsesStream(rec, req, resp, "owner-a", "resp_test", "deepseek-chat", "prompt", false, false, []string{"read_file"}, util.DefaultToolChoicePolicy(), "")
	body := rec.Body.String()
	if strings.Contains(body, "event: response.function_call_arguments.done") {
		t.Fatalf("did not expect function_call events for unknown tool, body=%s", body)
	}
}

func TestHandleResponsesNonStreamRequiredToolChoiceViolation(t *testing.T) {
	h := &Handler{}
	rec := httptest.NewRecorder()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			`data: {"p":"response/content","v":"plain text only"}` + "\n" +
				`data: [DONE]` + "\n",
		)),
	}
	policy := util.ToolChoicePolicy{
		Mode:    util.ToolChoiceRequired,
		Allowed: map[string]struct{}{"read_file": {}},
	}

	h.handleResponsesNonStream(rec, resp, "owner-a", "resp_test", "deepseek-chat", "prompt", false, []string{"read_file"}, policy, "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for required tool_choice violation, got %d body=%s", rec.Code, rec.Body.String())
	}
	out := decodeJSONBody(t, rec.Body.String())
	errObj, _ := out["error"].(map[string]any)
	if asString(errObj["code"]) != "tool_choice_violation" {
		t.Fatalf("expected code=tool_choice_violation, got %#v", out)
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
