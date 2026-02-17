package config

type ModelInfo struct {
	ID         string `json:"id"`
	Object     string `json:"object"`
	Created    int64  `json:"created"`
	OwnedBy    string `json:"owned_by"`
	Permission []any  `json:"permission,omitempty"`
}

var DeepSeekModels = []ModelInfo{
	{ID: "deepseek-chat", Object: "model", Created: 1677610602, OwnedBy: "deepseek", Permission: []any{}},
	{ID: "deepseek-reasoner", Object: "model", Created: 1677610602, OwnedBy: "deepseek", Permission: []any{}},
	{ID: "deepseek-chat-search", Object: "model", Created: 1677610602, OwnedBy: "deepseek", Permission: []any{}},
	{ID: "deepseek-reasoner-search", Object: "model", Created: 1677610602, OwnedBy: "deepseek", Permission: []any{}},
}

var ClaudeModels = []ModelInfo{
	// Current aliases
	{ID: "claude-opus-4-6", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-sonnet-4-5", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-haiku-4-5", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},

	// Current snapshots
	{ID: "claude-opus-4-5-20251101", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-opus-4-1", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-opus-4-1-20250805", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-opus-4-0", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-opus-4-20250514", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-sonnet-4-5-20250929", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-sonnet-4-0", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-sonnet-4-20250514", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-haiku-4-5-20251001", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},

	// Claude 3.x (legacy/deprecated snapshots and aliases)
	{ID: "claude-3-7-sonnet-latest", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-7-sonnet-20250219", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-sonnet-latest", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-sonnet-20240620", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-sonnet-20241022", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-opus-20240229", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-sonnet-20240229", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-haiku-latest", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-haiku-20241022", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-haiku-20240307", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},

	// Claude 2.x and 1.x (retired but accepted for compatibility)
	{ID: "claude-2.1", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-2.0", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-1.3", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-1.2", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-1.1", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-1.0", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-instant-1.2", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-instant-1.1", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-instant-1.0", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
}

func GetModelConfig(model string) (thinking bool, search bool, ok bool) {
	switch lower(model) {
	case "deepseek-chat":
		return false, false, true
	case "deepseek-reasoner":
		return true, false, true
	case "deepseek-chat-search":
		return false, true, true
	case "deepseek-reasoner-search":
		return true, true, true
	default:
		return false, false, false
	}
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func OpenAIModelsResponse() map[string]any {
	return map[string]any{"object": "list", "data": DeepSeekModels}
}

func ClaudeModelsResponse() map[string]any {
	return map[string]any{"object": "list", "data": ClaudeModels}
}
