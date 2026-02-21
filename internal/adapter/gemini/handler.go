package gemini

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/auth"
	"ds2api/internal/deepseek"
	"ds2api/internal/sse"
	streamengine "ds2api/internal/stream"
	"ds2api/internal/util"
)

var writeJSON = util.WriteJSON

type Handler struct {
	Store ConfigReader
	Auth  AuthResolver
	DS    DeepSeekCaller
}

func RegisterRoutes(r chi.Router, h *Handler) {
	r.Post("/v1beta/models/{model}:generateContent", h.GenerateContent)
	r.Post("/v1beta/models/{model}:streamGenerateContent", h.StreamGenerateContent)
	r.Post("/v1/models/{model}:generateContent", h.GenerateContent)
	r.Post("/v1/models/{model}:streamGenerateContent", h.StreamGenerateContent)
}

func (h *Handler) GenerateContent(w http.ResponseWriter, r *http.Request) {
	h.handleGenerateContent(w, r, false)
}

func (h *Handler) StreamGenerateContent(w http.ResponseWriter, r *http.Request) {
	h.handleGenerateContent(w, r, true)
}

func (h *Handler) handleGenerateContent(w http.ResponseWriter, r *http.Request, stream bool) {
	a, err := h.Auth.Determine(r)
	if err != nil {
		status := http.StatusUnauthorized
		detail := err.Error()
		if err == auth.ErrNoAccount {
			status = http.StatusTooManyRequests
		}
		writeGeminiError(w, status, detail)
		return
	}
	defer h.Auth.Release(a)

	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeGeminiError(w, http.StatusBadRequest, "invalid json")
		return
	}

	routeModel := strings.TrimSpace(chi.URLParam(r, "model"))
	stdReq, err := normalizeGeminiRequest(h.Store, routeModel, req, stream)
	if err != nil {
		writeGeminiError(w, http.StatusBadRequest, err.Error())
		return
	}

	sessionID, err := h.DS.CreateSession(r.Context(), a, 3)
	if err != nil {
		if a.UseConfigToken {
			writeGeminiError(w, http.StatusUnauthorized, "Account token is invalid. Please re-login the account in admin.")
		} else {
			writeGeminiError(w, http.StatusUnauthorized, "Invalid token.")
		}
		return
	}
	pow, err := h.DS.GetPow(r.Context(), a, 3)
	if err != nil {
		writeGeminiError(w, http.StatusUnauthorized, "Failed to get PoW (invalid token or unknown error).")
		return
	}
	payload := stdReq.CompletionPayload(sessionID)
	resp, err := h.DS.CallCompletion(r.Context(), a, payload, pow, 3)
	if err != nil {
		writeGeminiError(w, http.StatusInternalServerError, "Failed to get completion.")
		return
	}

	if stream {
		h.handleStreamGenerateContent(w, r, resp, stdReq.ResponseModel, stdReq.FinalPrompt, stdReq.Thinking, stdReq.Search, stdReq.ToolNames)
		return
	}
	h.handleNonStreamGenerateContent(w, resp, stdReq.ResponseModel, stdReq.FinalPrompt, stdReq.Thinking, stdReq.ToolNames)
}

func (h *Handler) handleNonStreamGenerateContent(w http.ResponseWriter, resp *http.Response, model, finalPrompt string, thinkingEnabled bool, toolNames []string) {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		writeGeminiError(w, resp.StatusCode, strings.TrimSpace(string(body)))
		return
	}

	result := sse.CollectStream(resp, thinkingEnabled, true)
	writeJSON(w, http.StatusOK, buildGeminiGenerateContentResponse(model, finalPrompt, result.Thinking, result.Text, toolNames))
}

func (h *Handler) handleStreamGenerateContent(w http.ResponseWriter, r *http.Request, resp *http.Response, model, finalPrompt string, thinkingEnabled, searchEnabled bool, toolNames []string) {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		writeGeminiError(w, resp.StatusCode, strings.TrimSpace(string(body)))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	rc := http.NewResponseController(w)
	_, canFlush := w.(http.Flusher)
	runtime := newGeminiStreamRuntime(w, rc, canFlush, model, finalPrompt, thinkingEnabled, searchEnabled, toolNames)

	initialType := "text"
	if thinkingEnabled {
		initialType = "thinking"
	}
	streamengine.ConsumeSSE(streamengine.ConsumeConfig{
		Context:             r.Context(),
		Body:                resp.Body,
		ThinkingEnabled:     thinkingEnabled,
		InitialType:         initialType,
		KeepAliveInterval:   time.Duration(deepseek.KeepAliveTimeout) * time.Second,
		IdleTimeout:         time.Duration(deepseek.StreamIdleTimeout) * time.Second,
		MaxKeepAliveNoInput: deepseek.MaxKeepaliveCount,
	}, streamengine.ConsumeHooks{
		OnParsed: runtime.onParsed,
		OnFinalize: func(_ streamengine.StopReason, _ error) {
			runtime.finalize()
		},
	})
}

func buildGeminiGenerateContentResponse(model, finalPrompt, finalThinking, finalText string, toolNames []string) map[string]any {
	parts := buildGeminiPartsFromFinal(finalText, finalThinking, toolNames)
	usage := buildGeminiUsage(finalPrompt, finalThinking, finalText)
	return map[string]any{
		"candidates": []map[string]any{
			{
				"index": 0,
				"content": map[string]any{
					"role":  "model",
					"parts": parts,
				},
				"finishReason": "STOP",
			},
		},
		"modelVersion":  model,
		"usageMetadata": usage,
	}
}

func buildGeminiUsage(finalPrompt, finalThinking, finalText string) map[string]any {
	promptTokens := util.EstimateTokens(finalPrompt)
	reasoningTokens := util.EstimateTokens(finalThinking)
	completionTokens := util.EstimateTokens(finalText)
	return map[string]any{
		"promptTokenCount":     promptTokens,
		"candidatesTokenCount": reasoningTokens + completionTokens,
		"totalTokenCount":      promptTokens + reasoningTokens + completionTokens,
	}
}

func buildGeminiPartsFromFinal(finalText, finalThinking string, toolNames []string) []map[string]any {
	detected := util.ParseToolCalls(finalText, toolNames)
	if len(detected) == 0 && strings.TrimSpace(finalThinking) != "" {
		detected = util.ParseToolCalls(finalThinking, toolNames)
	}
	if len(detected) > 0 {
		parts := make([]map[string]any, 0, len(detected))
		for _, tc := range detected {
			parts = append(parts, map[string]any{
				"functionCall": map[string]any{
					"name": tc.Name,
					"args": tc.Input,
				},
			})
		}
		return parts
	}

	text := finalText
	if strings.TrimSpace(text) == "" {
		text = finalThinking
	}
	return []map[string]any{{"text": text}}
}

type geminiStreamRuntime struct {
	w        http.ResponseWriter
	rc       *http.ResponseController
	canFlush bool

	model       string
	finalPrompt string

	thinkingEnabled bool
	searchEnabled   bool
	bufferContent   bool
	toolNames       []string

	thinking strings.Builder
	text     strings.Builder
}

func newGeminiStreamRuntime(
	w http.ResponseWriter,
	rc *http.ResponseController,
	canFlush bool,
	model string,
	finalPrompt string,
	thinkingEnabled bool,
	searchEnabled bool,
	toolNames []string,
) *geminiStreamRuntime {
	return &geminiStreamRuntime{
		w:               w,
		rc:              rc,
		canFlush:        canFlush,
		model:           model,
		finalPrompt:     finalPrompt,
		thinkingEnabled: thinkingEnabled,
		searchEnabled:   searchEnabled,
		bufferContent:   len(toolNames) > 0,
		toolNames:       toolNames,
	}
}

func (s *geminiStreamRuntime) sendChunk(payload map[string]any) {
	b, _ := json.Marshal(payload)
	_, _ = s.w.Write([]byte("data: "))
	_, _ = s.w.Write(b)
	_, _ = s.w.Write([]byte("\n\n"))
	if s.canFlush {
		_ = s.rc.Flush()
	}
}

func (s *geminiStreamRuntime) onParsed(parsed sse.LineResult) streamengine.ParsedDecision {
	if !parsed.Parsed {
		return streamengine.ParsedDecision{}
	}
	if parsed.ContentFilter || parsed.ErrorMessage != "" || parsed.Stop {
		return streamengine.ParsedDecision{Stop: true}
	}

	contentSeen := false
	for _, p := range parsed.Parts {
		if p.Text == "" {
			continue
		}
		if p.Type != "thinking" && s.searchEnabled && sse.IsCitation(p.Text) {
			continue
		}
		contentSeen = true
		if p.Type == "thinking" {
			if s.thinkingEnabled {
				s.thinking.WriteString(p.Text)
			}
			continue
		}
		s.text.WriteString(p.Text)
		if s.bufferContent {
			continue
		}
		s.sendChunk(map[string]any{
			"candidates": []map[string]any{
				{
					"index": 0,
					"content": map[string]any{
						"role":  "model",
						"parts": []map[string]any{{"text": p.Text}},
					},
				},
			},
			"modelVersion": s.model,
		})
	}
	return streamengine.ParsedDecision{ContentSeen: contentSeen}
}

func (s *geminiStreamRuntime) finalize() {
	finalThinking := s.thinking.String()
	finalText := s.text.String()

	if s.bufferContent {
		parts := buildGeminiPartsFromFinal(finalText, finalThinking, s.toolNames)
		s.sendChunk(map[string]any{
			"candidates": []map[string]any{
				{
					"index": 0,
					"content": map[string]any{
						"role":  "model",
						"parts": parts,
					},
				},
			},
			"modelVersion": s.model,
		})
	}

	s.sendChunk(map[string]any{
		"candidates": []map[string]any{
			{
				"index":        0,
				"finishReason": "STOP",
			},
		},
		"modelVersion":  s.model,
		"usageMetadata": buildGeminiUsage(s.finalPrompt, finalThinking, finalText),
	})
}

func writeGeminiError(w http.ResponseWriter, status int, message string) {
	errorStatus := "INVALID_ARGUMENT"
	switch status {
	case http.StatusUnauthorized:
		errorStatus = "UNAUTHENTICATED"
	case http.StatusForbidden:
		errorStatus = "PERMISSION_DENIED"
	case http.StatusTooManyRequests:
		errorStatus = "RESOURCE_EXHAUSTED"
	case http.StatusNotFound:
		errorStatus = "NOT_FOUND"
	default:
		if status >= 500 {
			errorStatus = "INTERNAL"
		}
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    status,
			"message": message,
			"status":  errorStatus,
		},
	})
}
