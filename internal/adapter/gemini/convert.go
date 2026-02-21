package gemini

import (
	"encoding/json"
	"fmt"
	"strings"

	"ds2api/internal/adapter/openai"
	"ds2api/internal/config"
	"ds2api/internal/util"
)

func normalizeGeminiRequest(store ConfigReader, routeModel string, req map[string]any, stream bool) (util.StandardRequest, error) {
	requestedModel := strings.TrimSpace(routeModel)
	if requestedModel == "" {
		return util.StandardRequest{}, fmt.Errorf("model is required in request path")
	}

	resolvedModel, ok := config.ResolveModel(store, requestedModel)
	if !ok {
		return util.StandardRequest{}, fmt.Errorf("Model '%s' is not available.", requestedModel)
	}
	thinkingEnabled, searchEnabled, _ := config.GetModelConfig(resolvedModel)

	messagesRaw := geminiMessagesFromRequest(req)
	if len(messagesRaw) == 0 {
		return util.StandardRequest{}, fmt.Errorf("Request must include non-empty contents.")
	}

	toolsRaw := convertGeminiTools(req["tools"])
	finalPrompt, toolNames := openai.BuildPromptForAdapter(messagesRaw, toolsRaw, "")
	passThrough := collectGeminiPassThrough(req)

	return util.StandardRequest{
		Surface:        "google_gemini",
		RequestedModel: requestedModel,
		ResolvedModel:  resolvedModel,
		ResponseModel:  requestedModel,
		Messages:       messagesRaw,
		FinalPrompt:    finalPrompt,
		ToolNames:      toolNames,
		Stream:         stream,
		Thinking:       thinkingEnabled,
		Search:         searchEnabled,
		PassThrough:    passThrough,
	}, nil
}

func geminiMessagesFromRequest(req map[string]any) []any {
	out := make([]any, 0, 8)
	if sys := normalizeGeminiSystemInstruction(req["systemInstruction"]); strings.TrimSpace(sys) != "" {
		out = append(out, map[string]any{
			"role":    "system",
			"content": sys,
		})
	}

	contents, _ := req["contents"].([]any)
	for _, item := range contents {
		content, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := mapGeminiRole(content["role"])
		if role == "" {
			role = "user"
		}
		parts, _ := content["parts"].([]any)
		if len(parts) == 0 {
			if text := strings.TrimSpace(asString(content["text"])); text != "" {
				out = append(out, map[string]any{
					"role":    role,
					"content": text,
				})
			}
			continue
		}

		textParts := make([]string, 0, len(parts))
		flushText := func() {
			if len(textParts) == 0 {
				return
			}
			out = append(out, map[string]any{
				"role":    role,
				"content": strings.Join(textParts, "\n"),
			})
			textParts = textParts[:0]
		}

		for _, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok {
				continue
			}
			if text := strings.TrimSpace(asString(part["text"])); text != "" {
				textParts = append(textParts, text)
				continue
			}

			if fnCall, ok := part["functionCall"].(map[string]any); ok {
				flushText()
				if name := strings.TrimSpace(asString(fnCall["name"])); name != "" {
					callID := strings.TrimSpace(asString(fnCall["id"]))
					if callID == "" {
						callID = "call_gemini"
					}
					out = append(out, map[string]any{
						"role": "assistant",
						"tool_calls": []any{
							map[string]any{
								"id":   callID,
								"type": "function",
								"function": map[string]any{
									"name":      name,
									"arguments": stringifyJSON(fnCall["args"]),
								},
							},
						},
					})
				}
				continue
			}

			if fnResp, ok := part["functionResponse"].(map[string]any); ok {
				flushText()
				name := strings.TrimSpace(asString(fnResp["name"]))
				callID := strings.TrimSpace(asString(fnResp["id"]))
				if callID == "" {
					callID = strings.TrimSpace(asString(fnResp["callId"]))
				}
				if callID == "" {
					callID = strings.TrimSpace(asString(fnResp["tool_call_id"]))
				}
				if callID == "" {
					callID = "call_gemini"
				}
				content := fnResp["response"]
				if content == nil {
					content = fnResp["output"]
				}
				if content == nil {
					content = ""
				}
				msg := map[string]any{
					"role":         "tool",
					"tool_call_id": callID,
					"content":      content,
				}
				if name != "" {
					msg["name"] = name
				}
				out = append(out, msg)
			}
		}
		flushText()
	}
	return out
}

func normalizeGeminiSystemInstruction(raw any) string {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		if parts, ok := v["parts"].([]any); ok {
			texts := make([]string, 0, len(parts))
			for _, item := range parts {
				part, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if text := strings.TrimSpace(asString(part["text"])); text != "" {
					texts = append(texts, text)
				}
			}
			return strings.Join(texts, "\n")
		}
		if text := strings.TrimSpace(asString(v["text"])); text != "" {
			return text
		}
	}
	return ""
}

func mapGeminiRole(v any) string {
	switch strings.ToLower(strings.TrimSpace(asString(v))) {
	case "user":
		return "user"
	case "model", "assistant":
		return "assistant"
	case "system":
		return "system"
	default:
		return ""
	}
}

func convertGeminiTools(raw any) []any {
	tools, _ := raw.([]any)
	if len(tools) == 0 {
		return nil
	}
	out := make([]any, 0, len(tools))
	for _, item := range tools {
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}

		if fnDecls, ok := tool["functionDeclarations"].([]any); ok && len(fnDecls) > 0 {
			for _, declRaw := range fnDecls {
				decl, ok := declRaw.(map[string]any)
				if !ok {
					continue
				}
				name := strings.TrimSpace(asString(decl["name"]))
				if name == "" {
					continue
				}
				function := map[string]any{
					"name": name,
				}
				if desc := strings.TrimSpace(asString(decl["description"])); desc != "" {
					function["description"] = desc
				}
				if params, ok := decl["parameters"].(map[string]any); ok {
					function["parameters"] = params
				}
				out = append(out, map[string]any{
					"type":     "function",
					"function": function,
				})
			}
			continue
		}

		// OpenAI-style passthrough fallback.
		if _, ok := tool["function"].(map[string]any); ok {
			out = append(out, tool)
			continue
		}

		// Loose fallback for flattened function schema objects.
		name := strings.TrimSpace(asString(tool["name"]))
		if name == "" {
			continue
		}
		fn := map[string]any{"name": name}
		if desc := strings.TrimSpace(asString(tool["description"])); desc != "" {
			fn["description"] = desc
		}
		if params, ok := tool["parameters"].(map[string]any); ok {
			fn["parameters"] = params
		}
		out = append(out, map[string]any{
			"type":     "function",
			"function": fn,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func collectGeminiPassThrough(req map[string]any) map[string]any {
	cfg, _ := req["generationConfig"].(map[string]any)
	if len(cfg) == 0 {
		return nil
	}
	out := map[string]any{}
	if v, ok := cfg["temperature"]; ok {
		out["temperature"] = v
	}
	if v, ok := cfg["topP"]; ok {
		out["top_p"] = v
	}
	if v, ok := cfg["maxOutputTokens"]; ok {
		out["max_tokens"] = v
	}
	if v, ok := cfg["stopSequences"]; ok {
		out["stop"] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func stringifyJSON(v any) string {
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
