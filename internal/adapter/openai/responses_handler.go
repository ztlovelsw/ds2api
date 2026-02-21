package openai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ds2api/internal/auth"
	"ds2api/internal/deepseek"
	openaifmt "ds2api/internal/format/openai"
	"ds2api/internal/sse"
	streamengine "ds2api/internal/stream"
)

func (h *Handler) GetResponseByID(w http.ResponseWriter, r *http.Request) {
	a, err := h.Auth.DetermineCaller(r)
	if err != nil {
		writeOpenAIError(w, http.StatusUnauthorized, err.Error())
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "response_id"))
	if id == "" {
		writeOpenAIError(w, http.StatusBadRequest, "response_id is required.")
		return
	}
	owner := responseStoreOwner(a)
	if owner == "" {
		writeOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	st := h.getResponseStore()
	item, ok := st.get(owner, id)
	if !ok {
		writeOpenAIError(w, http.StatusNotFound, "Response not found.")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) Responses(w http.ResponseWriter, r *http.Request) {
	a, err := h.Auth.Determine(r)
	if err != nil {
		status := http.StatusUnauthorized
		detail := err.Error()
		if err == auth.ErrNoAccount {
			status = http.StatusTooManyRequests
		}
		writeOpenAIError(w, status, detail)
		return
	}
	defer h.Auth.Release(a)
	r = r.WithContext(auth.WithAuth(r.Context(), a))
	owner := responseStoreOwner(a)
	if owner == "" {
		writeOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json")
		return
	}
	stdReq, err := normalizeOpenAIResponsesRequest(h.Store, req, requestTraceID(r))
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error())
		return
	}

	sessionID, err := h.DS.CreateSession(r.Context(), a, 3)
	if err != nil {
		if a.UseConfigToken {
			writeOpenAIError(w, http.StatusUnauthorized, "Account token is invalid. Please re-login the account in admin.")
		} else {
			writeOpenAIError(w, http.StatusUnauthorized, "Invalid token. If this should be a DS2API key, add it to config.keys first.")
		}
		return
	}
	pow, err := h.DS.GetPow(r.Context(), a, 3)
	if err != nil {
		writeOpenAIError(w, http.StatusUnauthorized, "Failed to get PoW (invalid token or unknown error).")
		return
	}
	payload := stdReq.CompletionPayload(sessionID)
	resp, err := h.DS.CallCompletion(r.Context(), a, payload, pow, 3)
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, "Failed to get completion.")
		return
	}

	responseID := "resp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if stdReq.Stream {
		h.handleResponsesStream(w, r, resp, owner, responseID, stdReq.ResponseModel, stdReq.FinalPrompt, stdReq.Thinking, stdReq.Search, stdReq.ToolNames)
		return
	}
	h.handleResponsesNonStream(w, resp, owner, responseID, stdReq.ResponseModel, stdReq.FinalPrompt, stdReq.Thinking, stdReq.ToolNames)
}

func (h *Handler) handleResponsesNonStream(w http.ResponseWriter, resp *http.Response, owner, responseID, model, finalPrompt string, thinkingEnabled bool, toolNames []string) {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		writeOpenAIError(w, resp.StatusCode, strings.TrimSpace(string(body)))
		return
	}
	result := sse.CollectStream(resp, thinkingEnabled, true)
	responseObj := openaifmt.BuildResponseObject(responseID, model, finalPrompt, result.Thinking, result.Text, toolNames)
	h.getResponseStore().put(owner, responseID, responseObj)
	writeJSON(w, http.StatusOK, responseObj)
}

func (h *Handler) handleResponsesStream(w http.ResponseWriter, r *http.Request, resp *http.Response, owner, responseID, model, finalPrompt string, thinkingEnabled, searchEnabled bool, toolNames []string) {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		writeOpenAIError(w, resp.StatusCode, strings.TrimSpace(string(body)))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	_, canFlush := w.(http.Flusher)

	initialType := "text"
	if thinkingEnabled {
		initialType = "thinking"
	}
	bufferToolContent := len(toolNames) > 0 && h.toolcallFeatureMatchEnabled()
	emitEarlyToolDeltas := h.toolcallEarlyEmitHighConfidence()

	streamRuntime := newResponsesStreamRuntime(
		w,
		rc,
		canFlush,
		responseID,
		model,
		finalPrompt,
		thinkingEnabled,
		searchEnabled,
		toolNames,
		bufferToolContent,
		emitEarlyToolDeltas,
		func(obj map[string]any) {
			h.getResponseStore().put(owner, responseID, obj)
		},
	)
	streamRuntime.sendCreated()

	streamengine.ConsumeSSE(streamengine.ConsumeConfig{
		Context:             r.Context(),
		Body:                resp.Body,
		ThinkingEnabled:     thinkingEnabled,
		InitialType:         initialType,
		KeepAliveInterval:   time.Duration(deepseek.KeepAliveTimeout) * time.Second,
		IdleTimeout:         time.Duration(deepseek.StreamIdleTimeout) * time.Second,
		MaxKeepAliveNoInput: deepseek.MaxKeepaliveCount,
	}, streamengine.ConsumeHooks{
		OnParsed: streamRuntime.onParsed,
		OnFinalize: func(_ streamengine.StopReason, _ error) {
			streamRuntime.finalize()
		},
	})
}

func responsesMessagesFromRequest(req map[string]any) []any {
	if msgs, ok := req["messages"].([]any); ok && len(msgs) > 0 {
		return prependInstructionMessage(msgs, req["instructions"])
	}
	if rawInput, ok := req["input"]; ok {
		if msgs := normalizeResponsesInputAsMessages(rawInput); len(msgs) > 0 {
			return prependInstructionMessage(msgs, req["instructions"])
		}
	}
	return nil
}

func prependInstructionMessage(messages []any, instructions any) []any {
	sys, _ := instructions.(string)
	sys = strings.TrimSpace(sys)
	if sys == "" {
		return messages
	}
	out := make([]any, 0, len(messages)+1)
	out = append(out, map[string]any{"role": "system", "content": sys})
	out = append(out, messages...)
	return out
}

func normalizeResponsesInputAsMessages(input any) []any {
	switch v := input.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []any{map[string]any{"role": "user", "content": v}}
	case []any:
		return normalizeResponsesInputArray(v)
	case map[string]any:
		if msg := normalizeResponsesInputItem(v); msg != nil {
			return []any{msg}
		}
		if txt, _ := v["text"].(string); strings.TrimSpace(txt) != "" {
			return []any{map[string]any{"role": "user", "content": txt}}
		}
		if content, ok := v["content"]; ok {
			if strings.TrimSpace(normalizeOpenAIContentForPrompt(content)) != "" {
				return []any{map[string]any{"role": "user", "content": content}}
			}
		}
	}
	return nil
}

func normalizeResponsesInputArray(items []any) []any {
	if len(items) == 0 {
		return nil
	}
	out := make([]any, 0, len(items))
	fallbackParts := make([]string, 0, len(items))
	flushFallback := func() {
		if len(fallbackParts) == 0 {
			return
		}
		out = append(out, map[string]any{"role": "user", "content": strings.Join(fallbackParts, "\n")})
		fallbackParts = fallbackParts[:0]
	}

	for _, item := range items {
		switch x := item.(type) {
		case map[string]any:
			if msg := normalizeResponsesInputItem(x); msg != nil {
				flushFallback()
				out = append(out, msg)
				continue
			}
			if s := normalizeResponsesFallbackPart(x); s != "" {
				fallbackParts = append(fallbackParts, s)
			}
		default:
			if s := strings.TrimSpace(fmt.Sprintf("%v", item)); s != "" {
				fallbackParts = append(fallbackParts, s)
			}
		}
	}
	flushFallback()
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeResponsesInputItem(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}

	role := strings.ToLower(strings.TrimSpace(asString(m["role"])))
	if role != "" {
		content := m["content"]
		if content == nil {
			if txt, _ := m["text"].(string); strings.TrimSpace(txt) != "" {
				content = txt
			}
		}
		if content == nil {
			return nil
		}
		return map[string]any{
			"role":    role,
			"content": content,
		}
	}

	itemType := strings.ToLower(strings.TrimSpace(asString(m["type"])))
	switch itemType {
	case "message", "input_message":
		content := m["content"]
		if content == nil {
			if txt, _ := m["text"].(string); strings.TrimSpace(txt) != "" {
				content = txt
			}
		}
		if content == nil {
			return nil
		}
		role := strings.ToLower(strings.TrimSpace(asString(m["role"])))
		if role == "" {
			role = "user"
		}
		return map[string]any{
			"role":    role,
			"content": content,
		}
	case "function_call_output", "tool_result":
		content := m["output"]
		if content == nil {
			content = m["content"]
		}
		if content == nil {
			content = ""
		}
		out := map[string]any{
			"role":    "tool",
			"content": content,
		}
		if callID := strings.TrimSpace(asString(m["call_id"])); callID != "" {
			out["tool_call_id"] = callID
		} else if callID = strings.TrimSpace(asString(m["tool_call_id"])); callID != "" {
			out["tool_call_id"] = callID
		}
		if name := strings.TrimSpace(asString(m["name"])); name != "" {
			out["name"] = name
		} else if name = strings.TrimSpace(asString(m["tool_name"])); name != "" {
			out["name"] = name
		}
		return out
	case "function_call", "tool_call":
		name := strings.TrimSpace(asString(m["name"]))
		var fn map[string]any
		if rawFn, ok := m["function"].(map[string]any); ok {
			fn = rawFn
			if name == "" {
				name = strings.TrimSpace(asString(fn["name"]))
			}
		}
		if name == "" {
			return nil
		}

		var argsRaw any
		if v, ok := m["arguments"]; ok {
			argsRaw = v
		} else if v, ok := m["input"]; ok {
			argsRaw = v
		}
		if argsRaw == nil && fn != nil {
			if v, ok := fn["arguments"]; ok {
				argsRaw = v
			} else if v, ok := fn["input"]; ok {
				argsRaw = v
			}
		}

		functionPayload := map[string]any{
			"name":      name,
			"arguments": stringifyToolCallArguments(argsRaw),
		}
		call := map[string]any{
			"type":     "function",
			"function": functionPayload,
		}
		if callID := strings.TrimSpace(asString(m["call_id"])); callID != "" {
			call["id"] = callID
		} else if callID = strings.TrimSpace(asString(m["id"])); callID != "" {
			call["id"] = callID
		}
		return map[string]any{
			"role":       "assistant",
			"tool_calls": []any{call},
		}
	case "input_text":
		if txt, _ := m["text"].(string); strings.TrimSpace(txt) != "" {
			return map[string]any{
				"role":    "user",
				"content": txt,
			}
		}
	}

	if txt, _ := m["text"].(string); strings.TrimSpace(txt) != "" {
		return map[string]any{
			"role":    "user",
			"content": txt,
		}
	}
	if content, ok := m["content"]; ok {
		if strings.TrimSpace(normalizeOpenAIContentForPrompt(content)) != "" {
			return map[string]any{
				"role":    "user",
				"content": content,
			}
		}
	}
	return nil
}

func normalizeResponsesFallbackPart(m map[string]any) string {
	if m == nil {
		return ""
	}
	if t, _ := m["type"].(string); strings.EqualFold(strings.TrimSpace(t), "input_text") {
		if txt, _ := m["text"].(string); strings.TrimSpace(txt) != "" {
			return txt
		}
	}
	if txt, _ := m["text"].(string); strings.TrimSpace(txt) != "" {
		return txt
	}
	if content, ok := m["content"]; ok {
		if normalized := strings.TrimSpace(normalizeOpenAIContentForPrompt(content)); normalized != "" {
			return normalized
		}
	}
	return strings.TrimSpace(fmt.Sprintf("%v", m))
}

func stringifyToolCallArguments(v any) string {
	switch x := v.(type) {
	case nil:
		return "{}"
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return "{}"
		}
		return s
	default:
		b, err := json.Marshal(x)
		if err != nil || len(b) == 0 {
			return "{}"
		}
		return string(b)
	}
}
