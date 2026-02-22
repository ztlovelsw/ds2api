package openai

import "strings"

func extractJSONObjectFrom(text string, start int) (string, int, bool) {
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
				end := i + 1
				return text[start:end], end, true
			}
		}
	}
	return "", 0, false
}

func findObjectFieldValueStart(text string, objStart int, keys []string) (int, bool) {
	if objStart < 0 || objStart >= len(text) || text[objStart] != '{' {
		return 0, false
	}
	depth := 0
	quote := byte(0)
	escaped := false
	for i := objStart; i < len(text); i++ {
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
			if depth == 1 {
				key, end, ok := parseJSONStringLiteral(text, i)
				if !ok {
					return 0, false
				}
				j := skipSpaces(text, end)
				if j >= len(text) || text[j] != ':' {
					i = end - 1
					continue
				}
				j = skipSpaces(text, j+1)
				if j >= len(text) {
					return 0, false
				}
				if containsKey(keys, key) {
					return j, true
				}
				i = j - 1
				continue
			}
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
				break
			}
		}
	}
	return 0, false
}

func parseJSONStringLiteral(text string, start int) (string, int, bool) {
	if start < 0 || start >= len(text) || text[start] != '"' {
		return "", 0, false
	}
	var b strings.Builder
	escaped := false
	for i := start + 1; i < len(text); i++ {
		ch := text[i]
		if escaped {
			b.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			return b.String(), i + 1, true
		}
		b.WriteByte(ch)
	}
	return "", 0, false
}

func containsKey(keys []string, value string) bool {
	for _, k := range keys {
		if k == value {
			return true
		}
	}
	return false
}

func skipSpaces(text string, i int) int {
	for i < len(text) {
		switch text[i] {
		case ' ', '\t', '\n', '\r':
			i++
		default:
			return i
		}
	}
	return i
}
