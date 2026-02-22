package gemini

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/auth"
	"ds2api/internal/sse"
	"ds2api/internal/util"
)

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
