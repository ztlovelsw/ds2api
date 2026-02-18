package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ds2api/internal/auth"
	"ds2api/internal/config"
	"ds2api/internal/deepseek"
	"ds2api/internal/sse"
	"ds2api/internal/util"
)

// writeJSON is a package-internal alias kept to avoid mass-renaming across
// every call-site in this file. It delegates to the shared util version.
var writeJSON = util.WriteJSON

type Handler struct {
	Store *config.Store
	Auth  *auth.Resolver
	DS    *deepseek.Client

	leaseMu      sync.Mutex
	streamLeases map[string]streamLease
	responsesMu  sync.Mutex
	responses    *responseStore
}

type streamLease struct {
	Auth      *auth.RequestAuth
	ExpiresAt time.Time
}

func RegisterRoutes(r chi.Router, h *Handler) {
	r.Get("/v1/models", h.ListModels)
	r.Get("/v1/models/{model_id}", h.GetModel)
	r.Post("/v1/chat/completions", h.ChatCompletions)
	r.Post("/v1/responses", h.Responses)
	r.Get("/v1/responses/{response_id}", h.GetResponseByID)
	r.Post("/v1/embeddings", h.Embeddings)
}

func (h *Handler) ListModels(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, config.OpenAIModelsResponse())
}

func (h *Handler) GetModel(w http.ResponseWriter, r *http.Request) {
	modelID := strings.TrimSpace(chi.URLParam(r, "model_id"))
	model, ok := config.OpenAIModelByID(h.Store, modelID)
	if !ok {
		writeOpenAIError(w, http.StatusNotFound, "Model not found.")
		return
	}
	writeJSON(w, http.StatusOK, model)
}

func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	if isVercelStreamReleaseRequest(r) {
		h.handleVercelStreamRelease(w, r)
		return
	}
	if isVercelStreamPrepareRequest(r) {
		h.handleVercelStreamPrepare(w, r)
		return
	}

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

	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json")
		return
	}
	model, _ := req["model"].(string)
	messagesRaw, _ := req["messages"].([]any)
	if model == "" || len(messagesRaw) == 0 {
		writeOpenAIError(w, http.StatusBadRequest, "Request must include 'model' and 'messages'.")
		return
	}
	resolvedModel, ok := config.ResolveModel(h.Store, model)
	if !ok {
		writeOpenAIError(w, http.StatusBadRequest, fmt.Sprintf("Model '%s' is not available.", model))
		return
	}
	thinkingEnabled, searchEnabled, _ := config.GetModelConfig(resolvedModel)
	responseModel := strings.TrimSpace(model)
	if responseModel == "" {
		responseModel = resolvedModel
	}

	finalPrompt, toolNames := buildOpenAIFinalPrompt(messagesRaw, req["tools"])

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
	payload := map[string]any{
		"chat_session_id":   sessionID,
		"parent_message_id": nil,
		"prompt":            finalPrompt,
		"ref_file_ids":      []any{},
		"thinking_enabled":  thinkingEnabled,
		"search_enabled":    searchEnabled,
	}
	applyOpenAIChatPassThrough(req, payload)
	resp, err := h.DS.CallCompletion(r.Context(), a, payload, pow, 3)
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, "Failed to get completion.")
		return
	}
	if util.ToBool(req["stream"]) {
		h.handleStream(w, r, resp, sessionID, responseModel, finalPrompt, thinkingEnabled, searchEnabled, toolNames)
		return
	}
	h.handleNonStream(w, r.Context(), resp, sessionID, responseModel, finalPrompt, thinkingEnabled, toolNames)
}

func (h *Handler) handleNonStream(w http.ResponseWriter, ctx context.Context, resp *http.Response, completionID, model, finalPrompt string, thinkingEnabled bool, toolNames []string) {
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		writeOpenAIError(w, resp.StatusCode, string(body))
		return
	}
	_ = ctx
	result := sse.CollectStream(resp, thinkingEnabled, true)

	finalThinking := result.Thinking
	finalText := result.Text
	detected := util.ParseToolCalls(finalText, toolNames)
	finishReason := "stop"
	messageObj := map[string]any{"role": "assistant", "content": finalText}
	if thinkingEnabled && finalThinking != "" {
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

	writeJSON(w, http.StatusOK, map[string]any{
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
	})
}

func (h *Handler) handleStream(w http.ResponseWriter, r *http.Request, resp *http.Response, completionID, model, finalPrompt string, thinkingEnabled, searchEnabled bool, toolNames []string) {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		writeOpenAIError(w, resp.StatusCode, string(body))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	canFlush := rc.Flush() == nil
	if !canFlush {
		config.Logger.Warn("[stream] response writer does not support flush; streaming may be buffered")
	}

	created := time.Now().Unix()
	firstChunkSent := false
	bufferToolContent := len(toolNames) > 0
	var toolSieve toolStreamSieveState
	toolCallsEmitted := false
	streamToolCallIDs := map[int]string{}
	initialType := "text"
	if thinkingEnabled {
		initialType = "thinking"
	}
	parsedLines, done := sse.StartParsedLinePump(r.Context(), resp.Body, thinkingEnabled, initialType)
	thinking := strings.Builder{}
	text := strings.Builder{}
	lastContent := time.Now()
	hasContent := false
	keepaliveTicker := time.NewTicker(time.Duration(deepseek.KeepAliveTimeout) * time.Second)
	defer keepaliveTicker.Stop()
	keepaliveCountWithoutContent := 0

	sendChunk := func(v any) {
		b, _ := json.Marshal(v)
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(b)
		_, _ = w.Write([]byte("\n\n"))
		if canFlush {
			_ = rc.Flush()
		}
	}
	sendDone := func() {
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		if canFlush {
			_ = rc.Flush()
		}
	}

	finalize := func(finishReason string) {
		finalThinking := thinking.String()
		finalText := text.String()
		detected := util.ParseToolCalls(finalText, toolNames)
		if len(detected) > 0 && !toolCallsEmitted {
			finishReason = "tool_calls"
			delta := map[string]any{
				"tool_calls": util.FormatOpenAIStreamToolCalls(detected),
			}
			if !firstChunkSent {
				delta["role"] = "assistant"
				firstChunkSent = true
			}
			sendChunk(map[string]any{
				"id":      completionID,
				"object":  "chat.completion.chunk",
				"created": created,
				"model":   model,
				"choices": []map[string]any{{"delta": delta, "index": 0}},
			})
		} else if bufferToolContent {
			for _, evt := range flushToolSieve(&toolSieve, toolNames) {
				if evt.Content == "" {
					continue
				}
				delta := map[string]any{
					"content": evt.Content,
				}
				if !firstChunkSent {
					delta["role"] = "assistant"
					firstChunkSent = true
				}
				sendChunk(map[string]any{
					"id":      completionID,
					"object":  "chat.completion.chunk",
					"created": created,
					"model":   model,
					"choices": []map[string]any{{"delta": delta, "index": 0}},
				})
			}
		}
		if len(detected) > 0 || toolCallsEmitted {
			finishReason = "tool_calls"
		}
		promptTokens := util.EstimateTokens(finalPrompt)
		reasoningTokens := util.EstimateTokens(finalThinking)
		completionTokens := util.EstimateTokens(finalText)
		sendChunk(map[string]any{
			"id":      completionID,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []map[string]any{{"delta": map[string]any{}, "index": 0, "finish_reason": finishReason}},
			"usage": map[string]any{
				"prompt_tokens":     promptTokens,
				"completion_tokens": reasoningTokens + completionTokens,
				"total_tokens":      promptTokens + reasoningTokens + completionTokens,
				"completion_tokens_details": map[string]any{
					"reasoning_tokens": reasoningTokens,
				},
			},
		})
		sendDone()
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepaliveTicker.C:
			if !hasContent {
				keepaliveCountWithoutContent++
				if keepaliveCountWithoutContent >= deepseek.MaxKeepaliveCount {
					finalize("stop")
					return
				}
			}
			if hasContent && time.Since(lastContent) > time.Duration(deepseek.StreamIdleTimeout)*time.Second {
				finalize("stop")
				return
			}
			if canFlush {
				_, _ = w.Write([]byte(": keep-alive\n\n"))
				_ = rc.Flush()
			}
		case parsed, ok := <-parsedLines:
			if !ok {
				// Ensure scanner completion is observed only after all queued
				// SSE lines are drained, avoiding early finalize races.
				_ = <-done
				finalize("stop")
				return
			}
			if !parsed.Parsed {
				continue
			}
			if parsed.ContentFilter || parsed.ErrorMessage != "" {
				finalize("content_filter")
				return
			}
			if parsed.Stop {
				finalize("stop")
				return
			}
			newChoices := make([]map[string]any, 0, len(parsed.Parts))
			for _, p := range parsed.Parts {
				if searchEnabled && sse.IsCitation(p.Text) {
					continue
				}
				if p.Text == "" {
					continue
				}
				hasContent = true
				lastContent = time.Now()
				keepaliveCountWithoutContent = 0
				delta := map[string]any{}
				if !firstChunkSent {
					delta["role"] = "assistant"
					firstChunkSent = true
				}
				if p.Type == "thinking" {
					if thinkingEnabled {
						thinking.WriteString(p.Text)
						delta["reasoning_content"] = p.Text
					}
				} else {
					text.WriteString(p.Text)
					if !bufferToolContent {
						delta["content"] = p.Text
					} else {
						events := processToolSieveChunk(&toolSieve, p.Text, toolNames)
						if len(events) == 0 {
							// Keep thinking delta only frame.
						}
						for _, evt := range events {
							if len(evt.ToolCallDeltas) > 0 {
								toolCallsEmitted = true
								tcDelta := map[string]any{
									"tool_calls": formatIncrementalStreamToolCallDeltas(evt.ToolCallDeltas, streamToolCallIDs),
								}
								if !firstChunkSent {
									tcDelta["role"] = "assistant"
									firstChunkSent = true
								}
								newChoices = append(newChoices, map[string]any{
									"delta": tcDelta,
									"index": 0,
								})
								continue
							}
							if len(evt.ToolCalls) > 0 {
								toolCallsEmitted = true
								tcDelta := map[string]any{
									"tool_calls": util.FormatOpenAIStreamToolCalls(evt.ToolCalls),
								}
								if !firstChunkSent {
									tcDelta["role"] = "assistant"
									firstChunkSent = true
								}
								newChoices = append(newChoices, map[string]any{
									"delta": tcDelta,
									"index": 0,
								})
								continue
							}
							if evt.Content != "" {
								contentDelta := map[string]any{
									"content": evt.Content,
								}
								if !firstChunkSent {
									contentDelta["role"] = "assistant"
									firstChunkSent = true
								}
								newChoices = append(newChoices, map[string]any{
									"delta": contentDelta,
									"index": 0,
								})
							}
						}
					}
				}
				if len(delta) > 0 {
					newChoices = append(newChoices, map[string]any{"delta": delta, "index": 0})
				}
			}
			if len(newChoices) > 0 {
				sendChunk(map[string]any{
					"id":      completionID,
					"object":  "chat.completion.chunk",
					"created": created,
					"model":   model,
					"choices": newChoices,
				})
			}
		}
	}
}

func injectToolPrompt(messages []map[string]any, tools []any) ([]map[string]any, []string) {
	toolSchemas := make([]string, 0, len(tools))
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		tool, ok := t.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := tool["function"].(map[string]any)
		if len(fn) == 0 {
			fn = tool
		}
		name, _ := fn["name"].(string)
		desc, _ := fn["description"].(string)
		schema, _ := fn["parameters"].(map[string]any)
		if name == "" {
			name = "unknown"
		}
		names = append(names, name)
		if desc == "" {
			desc = "No description available"
		}
		b, _ := json.Marshal(schema)
		toolSchemas = append(toolSchemas, fmt.Sprintf("Tool: %s\nDescription: %s\nParameters: %s", name, desc, string(b)))
	}
	if len(toolSchemas) == 0 {
		return messages, names
	}
	toolPrompt := "You have access to these tools:\n\n" + strings.Join(toolSchemas, "\n\n") + "\n\nWhen you need to use tools, output ONLY this JSON format (no other text):\n{\"tool_calls\": [{\"name\": \"tool_name\", \"input\": {\"param\": \"value\"}}]}\n\nIMPORTANT:\n1) If calling tools, output ONLY the JSON. The response must start with { and end with }.\n2) After receiving a tool result, you MUST use it to produce the final answer.\n3) Only call another tool when the previous result is missing required data or returned an error."

	for i := range messages {
		if messages[i]["role"] == "system" {
			old, _ := messages[i]["content"].(string)
			messages[i]["content"] = strings.TrimSpace(old + "\n\n" + toolPrompt)
			return messages, names
		}
	}
	messages = append([]map[string]any{{"role": "system", "content": toolPrompt}}, messages...)
	return messages, names
}

func formatIncrementalStreamToolCallDeltas(deltas []toolCallDelta, ids map[int]string) []map[string]any {
	if len(deltas) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(deltas))
	for _, d := range deltas {
		if d.Name == "" && d.Arguments == "" {
			continue
		}
		callID, ok := ids[d.Index]
		if !ok || callID == "" {
			callID = "call_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			ids[d.Index] = callID
		}
		item := map[string]any{
			"index": d.Index,
			"id":    callID,
			"type":  "function",
		}
		fn := map[string]any{}
		if d.Name != "" {
			fn["name"] = d.Name
		}
		if d.Arguments != "" {
			fn["arguments"] = d.Arguments
		}
		if len(fn) > 0 {
			item["function"] = fn
		}
		out = append(out, item)
	}
	return out
}

func writeOpenAIError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    openAIErrorType(status),
			"code":    openAIErrorCode(status),
			"param":   nil,
		},
	})
}

func openAIErrorType(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusServiceUnavailable:
		return "service_unavailable_error"
	default:
		if status >= 500 {
			return "api_error"
		}
		return "invalid_request_error"
	}
}

func openAIErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request"
	case http.StatusUnauthorized:
		return "authentication_failed"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusTooManyRequests:
		return "rate_limit_exceeded"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusServiceUnavailable:
		return "service_unavailable"
	default:
		if status >= 500 {
			return "internal_error"
		}
		return "invalid_request"
	}
}

func applyOpenAIChatPassThrough(req map[string]any, payload map[string]any) {
	for _, k := range []string{
		"temperature",
		"top_p",
		"max_tokens",
		"max_completion_tokens",
		"presence_penalty",
		"frequency_penalty",
		"stop",
	} {
		if v, ok := req[k]; ok {
			payload[k] = v
		}
	}
}
