package openai

import (
	"strings"

	"ds2api/internal/util"
)

type toolStreamSieveState struct {
	pending   strings.Builder
	capture   strings.Builder
	capturing bool
}

type toolStreamEvent struct {
	Content   string
	ToolCalls []util.ParsedToolCall
}

func processToolSieveChunk(state *toolStreamSieveState, chunk string, toolNames []string) []toolStreamEvent {
	if state == nil {
		return nil
	}
	if chunk != "" {
		state.pending.WriteString(chunk)
	}
	events := make([]toolStreamEvent, 0, 2)

	for {
		if state.capturing {
			if state.pending.Len() > 0 {
				state.capture.WriteString(state.pending.String())
				state.pending.Reset()
			}
			prefix, calls, suffix, ready := consumeToolCapture(state.capture.String(), toolNames)
			if !ready {
				break
			}
			state.capture.Reset()
			state.capturing = false
			if prefix != "" {
				events = append(events, toolStreamEvent{Content: prefix})
			}
			if len(calls) > 0 {
				events = append(events, toolStreamEvent{ToolCalls: calls})
			}
			if suffix != "" {
				state.pending.WriteString(suffix)
			}
			continue
		}

		pending := state.pending.String()
		if pending == "" {
			break
		}
		start := findToolSegmentStart(pending)
		if start >= 0 {
			prefix := pending[:start]
			if prefix != "" {
				events = append(events, toolStreamEvent{Content: prefix})
			}
			state.pending.Reset()
			state.capture.WriteString(pending[start:])
			state.capturing = true
			continue
		}

		safe, hold := splitSafeContentForToolDetection(pending)
		if safe == "" {
			break
		}
		state.pending.Reset()
		state.pending.WriteString(hold)
		events = append(events, toolStreamEvent{Content: safe})
	}

	return events
}

func flushToolSieve(state *toolStreamSieveState, toolNames []string) []toolStreamEvent {
	if state == nil {
		return nil
	}
	events := processToolSieveChunk(state, "", toolNames)
	if state.capturing {
		consumedPrefix, consumedCalls, consumedSuffix, ready := consumeToolCapture(state.capture.String(), toolNames)
		if ready {
			if consumedPrefix != "" {
				events = append(events, toolStreamEvent{Content: consumedPrefix})
			}
			if len(consumedCalls) > 0 {
				events = append(events, toolStreamEvent{ToolCalls: consumedCalls})
			}
			if consumedSuffix != "" {
				events = append(events, toolStreamEvent{Content: consumedSuffix})
			}
		} else {
			// Incomplete captured tool JSON at stream end: suppress raw capture.
		}
		state.capture.Reset()
		state.capturing = false
	}
	if state.pending.Len() > 0 {
		events = append(events, toolStreamEvent{Content: state.pending.String()})
		state.pending.Reset()
	}
	return events
}

func splitSafeContentForToolDetection(s string) (safe, hold string) {
	if s == "" {
		return "", ""
	}
	suspiciousStart := findSuspiciousPrefixStart(s)
	if suspiciousStart < 0 {
		return s, ""
	}
	if suspiciousStart > 0 {
		return s[:suspiciousStart], s[suspiciousStart:]
	}
	// If suspicious content starts at position 0, keep holding until we can
	// parse a complete tool JSON block or reach stream flush.
	return "", s
}

func findSuspiciousPrefixStart(s string) int {
	start := -1
	indices := []int{
		strings.LastIndex(s, "{"),
		strings.LastIndex(s, "["),
		strings.LastIndex(s, "```"),
	}
	for _, idx := range indices {
		if idx > start {
			start = idx
		}
	}
	return start
}

func findToolSegmentStart(s string) int {
	if s == "" {
		return -1
	}
	lower := strings.ToLower(s)
	keyIdx := strings.Index(lower, "tool_calls")
	if keyIdx < 0 {
		return -1
	}
	if start := strings.LastIndex(s[:keyIdx], "{"); start >= 0 {
		return start
	}
	return keyIdx
}

func consumeToolCapture(captured string, toolNames []string) (prefix string, calls []util.ParsedToolCall, suffix string, ready bool) {
	if captured == "" {
		return "", nil, "", false
	}
	lower := strings.ToLower(captured)
	keyIdx := strings.Index(lower, "tool_calls")
	if keyIdx < 0 {
		return "", nil, "", false
	}
	start := strings.LastIndex(captured[:keyIdx], "{")
	if start < 0 {
		return "", nil, "", false
	}
	obj, end, ok := extractJSONObjectFrom(captured, start)
	if !ok {
		return "", nil, "", false
	}
	parsed := util.ParseToolCalls(obj, toolNames)
	if len(parsed) == 0 {
		// `tool_calls` key exists but strict JSON parse failed.
		// Drop the captured object body to avoid leaking raw tool JSON.
		return captured[:start], nil, captured[end:], true
	}
	return captured[:start], parsed, captured[end:], true
}

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
