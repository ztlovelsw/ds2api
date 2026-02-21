package openai

import (
	"ds2api/internal/deepseek"
)

func buildOpenAIFinalPrompt(messagesRaw []any, toolsRaw any, traceID string) (string, []string) {
	messages := normalizeOpenAIMessagesForPrompt(messagesRaw, traceID)
	toolNames := []string{}
	if tools, ok := toolsRaw.([]any); ok && len(tools) > 0 {
		messages, toolNames = injectToolPrompt(messages, tools)
	}
	return deepseek.MessagesPrepare(messages), toolNames
}

// BuildPromptForAdapter exposes the OpenAI-compatible prompt building flow so
// other protocol adapters (for example Gemini) can reuse the same tool/history
// normalization logic and remain behavior-compatible with chat/completions.
func BuildPromptForAdapter(messagesRaw []any, toolsRaw any, traceID string) (string, []string) {
	return buildOpenAIFinalPrompt(messagesRaw, toolsRaw, traceID)
}
