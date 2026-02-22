package openai

import (
	"encoding/json"

	openaifmt "ds2api/internal/format/openai"
)

func (s *responsesStreamRuntime) sendEvent(event string, payload map[string]any) {
	b, _ := json.Marshal(payload)
	_, _ = s.w.Write([]byte("event: " + event + "\n"))
	_, _ = s.w.Write([]byte("data: "))
	_, _ = s.w.Write(b)
	_, _ = s.w.Write([]byte("\n\n"))
	if s.canFlush {
		_ = s.rc.Flush()
	}
}

func (s *responsesStreamRuntime) sendCreated() {
	s.sendEvent("response.created", openaifmt.BuildResponsesCreatedPayload(s.responseID, s.model))
}

func (s *responsesStreamRuntime) sendDone() {
	_, _ = s.w.Write([]byte("data: [DONE]\n\n"))
	if s.canFlush {
		_ = s.rc.Flush()
	}
}

func (s *responsesStreamRuntime) processToolStreamEvents(events []toolStreamEvent, emitContent bool) {
	for _, evt := range events {
		if emitContent && evt.Content != "" {
			s.sendEvent("response.output_text.delta", openaifmt.BuildResponsesTextDeltaPayload(s.responseID, evt.Content))
		}
		if len(evt.ToolCallDeltas) > 0 {
			if !s.emitEarlyToolDeltas {
				continue
			}
			formatted := formatIncrementalStreamToolCallDeltas(evt.ToolCallDeltas, s.streamToolCallIDs)
			if len(formatted) == 0 {
				continue
			}
			s.toolCallsEmitted = true
			s.sendEvent("response.output_tool_call.delta", openaifmt.BuildResponsesToolCallDeltaPayload(s.responseID, formatted))
			s.emitFunctionCallDeltaEvents(evt.ToolCallDeltas)
		}
		if len(evt.ToolCalls) > 0 {
			s.emitToolCallsDone(evt.ToolCalls)
		}
	}
}
