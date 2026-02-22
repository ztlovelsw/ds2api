package util

import (
	"regexp"
	"strings"
)

var toolCallPattern = regexp.MustCompile(`\{\s*["']tool_calls["']\s*:\s*\[(.*?)\]\s*\}`)
var fencedJSONPattern = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)\\s*```")
var fencedBlockPattern = regexp.MustCompile("(?s)```.*?```")

func buildToolCallCandidates(text string) []string {
	trimmed := strings.TrimSpace(text)
	candidates := []string{trimmed}

	// fenced code block candidates: ```json ... ```
	for _, match := range fencedJSONPattern.FindAllStringSubmatch(trimmed, -1) {
		if len(match) >= 2 {
			candidates = append(candidates, strings.TrimSpace(match[1]))
		}
	}

	// best-effort extraction around "tool_calls" key in mixed text payloads.
	candidates = append(candidates, extractToolCallObjects(trimmed)...)

	// best-effort object slice: from first '{' to last '}'
	first := strings.Index(trimmed, "{")
	last := strings.LastIndex(trimmed, "}")
	if first >= 0 && last > first {
		candidates = append(candidates, strings.TrimSpace(trimmed[first:last+1]))
	}

	// legacy regex extraction fallback
	if m := toolCallPattern.FindStringSubmatch(trimmed); len(m) >= 2 {
		candidates = append(candidates, "{"+`"tool_calls":[`+m[1]+"]}")
	}

	uniq := make([]string, 0, len(candidates))
	seen := map[string]struct{}{}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		uniq = append(uniq, c)
	}
	return uniq
}

func extractToolCallObjects(text string) []string {
	if text == "" {
		return nil
	}
	lower := strings.ToLower(text)
	out := []string{}
	offset := 0
	for {
		idx := strings.Index(lower[offset:], "tool_calls")
		if idx < 0 {
			break
		}
		idx += offset
		start := strings.LastIndex(text[:idx], "{")
		for start >= 0 {
			candidate, end, ok := extractJSONObject(text, start)
			if ok {
				// Move forward to avoid repeatedly matching the same object.
				offset = end
				out = append(out, strings.TrimSpace(candidate))
				break
			}
			start = strings.LastIndex(text[:start], "{")
		}
		if start < 0 {
			offset = idx + len("tool_calls")
		}
	}
	return out
}

func extractJSONObject(text string, start int) (string, int, bool) {
	if start < 0 || start >= len(text) || text[start] != '{' {
		return "", 0, false
	}
	depth := 0
	quote := byte(0)
	escaped := false
	for i := start; i < len(text); i++ {
		ch := text[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if ch == '{' {
			depth++
			continue
		}
		if ch == '}' {
			depth--
			if depth == 0 {
				return text[start : i+1], i + 1, true
			}
		}
	}
	return "", 0, false
}

func looksLikeToolExampleContext(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return false
	}
	return strings.Contains(t, "```")
}

func stripFencedCodeBlocks(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return fencedBlockPattern.ReplaceAllString(text, " ")
}
