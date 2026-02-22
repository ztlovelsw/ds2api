package openai

import (
	"strings"

	"ds2api/internal/util"
)

type toolStreamSieveState struct {
	pending        strings.Builder
	capture        strings.Builder
	capturing      bool
	recentTextTail string
	disableDeltas  bool
	toolNameSent   bool
	toolName       string
	toolArgsStart  int
	toolArgsSent   int
	toolArgsString bool
	toolArgsDone   bool
}

type toolStreamEvent struct {
	Content        string
	ToolCalls      []util.ParsedToolCall
	ToolCallDeltas []toolCallDelta
}

type toolCallDelta struct {
	Index     int
	Name      string
	Arguments string
}

const toolSieveCaptureLimit = 8 * 1024
const toolSieveContextTailLimit = 256

func (s *toolStreamSieveState) resetIncrementalToolState() {
	s.disableDeltas = false
	s.toolNameSent = false
	s.toolName = ""
	s.toolArgsStart = -1
	s.toolArgsSent = -1
	s.toolArgsString = false
	s.toolArgsDone = false
}

func (s *toolStreamSieveState) noteText(content string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	s.recentTextTail = appendTail(s.recentTextTail, content, toolSieveContextTailLimit)
}

func appendTail(prev, next string, max int) string {
	if max <= 0 {
		return ""
	}
	combined := prev + next
	if len(combined) <= max {
		return combined
	}
	return combined[len(combined)-max:]
}

func looksLikeToolExampleContext(text string) bool {
	return insideCodeFence(text)
}

func insideCodeFence(text string) bool {
	if text == "" {
		return false
	}
	return strings.Count(text, "```")%2 == 1
}
