package util

import (
	"encoding/json"
	"strings"
)

type ParsedToolCall struct {
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

func ParseToolCalls(text string, availableToolNames []string) []ParsedToolCall {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	text = stripFencedCodeBlocks(text)
	if strings.TrimSpace(text) == "" {
		return nil
	}

	candidates := buildToolCallCandidates(text)
	var parsed []ParsedToolCall
	for _, candidate := range candidates {
		if tc := parseToolCallsPayload(candidate); len(tc) > 0 {
			parsed = tc
			break
		}
	}
	if len(parsed) == 0 {
		return nil
	}

	return filterToolCalls(parsed, availableToolNames)
}

func ParseStandaloneToolCalls(text string, availableToolNames []string) []ParsedToolCall {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	if looksLikeToolExampleContext(trimmed) {
		return nil
	}
	candidates := []string{trimmed}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if !strings.HasPrefix(candidate, "{") && !strings.HasPrefix(candidate, "[") {
			continue
		}
		if parsed := parseToolCallsPayload(candidate); len(parsed) > 0 {
			return filterToolCalls(parsed, availableToolNames)
		}
	}
	return nil
}

func filterToolCalls(parsed []ParsedToolCall, availableToolNames []string) []ParsedToolCall {
	allowed := map[string]struct{}{}
	for _, name := range availableToolNames {
		allowed[name] = struct{}{}
	}
	out := make([]ParsedToolCall, 0, len(parsed))
	for _, tc := range parsed {
		if tc.Name == "" {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[tc.Name]; !ok {
				continue
			}
		}
		if tc.Input == nil {
			tc.Input = map[string]any{}
		}
		out = append(out, tc)
	}
	// If the model clearly emitted tool_calls JSON but all names are outside the
	// declared set, keep the parsed calls as a fallback so upper layers can still
	// intercept structured tool output instead of leaking raw JSON to users.
	if len(out) == 0 && len(parsed) > 0 {
		for _, tc := range parsed {
			if tc.Name == "" {
				continue
			}
			if tc.Input == nil {
				tc.Input = map[string]any{}
			}
			out = append(out, tc)
		}
	}
	return out
}

func parseToolCallsPayload(payload string) []ParsedToolCall {
	var decoded any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		return nil
	}
	switch v := decoded.(type) {
	case map[string]any:
		if tc, ok := v["tool_calls"]; ok {
			return parseToolCallList(tc)
		}
		if parsed, ok := parseToolCallItem(v); ok {
			return []ParsedToolCall{parsed}
		}
	case []any:
		return parseToolCallList(v)
	}
	return nil
}

func parseToolCallList(v any) []ParsedToolCall {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]ParsedToolCall, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if tc, ok := parseToolCallItem(m); ok {
			out = append(out, tc)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseToolCallItem(m map[string]any) (ParsedToolCall, bool) {
	name, _ := m["name"].(string)
	inputRaw, hasInput := m["input"]
	if fn, ok := m["function"].(map[string]any); ok {
		if name == "" {
			name, _ = fn["name"].(string)
		}
		if !hasInput {
			if v, ok := fn["arguments"]; ok {
				inputRaw = v
				hasInput = true
			}
		}
	}
	if !hasInput {
		for _, key := range []string{"arguments", "args", "parameters", "params"} {
			if v, ok := m[key]; ok {
				inputRaw = v
				hasInput = true
				break
			}
		}
	}
	if strings.TrimSpace(name) == "" {
		return ParsedToolCall{}, false
	}
	return ParsedToolCall{
		Name:  strings.TrimSpace(name),
		Input: parseToolCallInput(inputRaw),
	}, true
}

func parseToolCallInput(v any) map[string]any {
	switch x := v.(type) {
	case nil:
		return map[string]any{}
	case map[string]any:
		return x
	case string:
		raw := strings.TrimSpace(x)
		if raw == "" {
			return map[string]any{}
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil && parsed != nil {
			return parsed
		}
		return map[string]any{"_raw": raw}
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return map[string]any{}
		}
		var parsed map[string]any
		if err := json.Unmarshal(b, &parsed); err == nil && parsed != nil {
			return parsed
		}
		return map[string]any{}
	}
}
