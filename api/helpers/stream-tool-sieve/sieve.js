'use strict';

const {
  TOOL_SIEVE_CAPTURE_LIMIT,
  resetIncrementalToolState,
  noteText,
  insideCodeFence,
} = require('./state');
const {
  buildIncrementalToolDeltas,
} = require('./incremental');
const {
  parseStandaloneToolCalls,
} = require('./parse');
const {
  extractJSONObjectFrom,
} = require('./jsonscan');

function processToolSieveChunk(state, chunk, toolNames) {
  if (!state) {
    return [];
  }
  if (chunk) {
    state.pending += chunk;
  }
  const events = [];
  // eslint-disable-next-line no-constant-condition
  while (true) {
    if (state.capturing) {
      if (state.pending) {
        state.capture += state.pending;
        state.pending = '';
      }
      const deltas = buildIncrementalToolDeltas(state);
      if (deltas.length > 0) {
        events.push({ type: 'tool_call_deltas', deltas });
      }
      const consumed = consumeToolCapture(state, toolNames);
      if (!consumed.ready) {
        if (state.capture.length > TOOL_SIEVE_CAPTURE_LIMIT) {
          noteText(state, state.capture);
          events.push({ type: 'text', text: state.capture });
          state.capture = '';
          state.capturing = false;
          resetIncrementalToolState(state);
          continue;
        }
        break;
      }
      state.capture = '';
      state.capturing = false;
      resetIncrementalToolState(state);
      if (consumed.prefix) {
        noteText(state, consumed.prefix);
        events.push({ type: 'text', text: consumed.prefix });
      }
      if (Array.isArray(consumed.calls) && consumed.calls.length > 0) {
        events.push({ type: 'tool_calls', calls: consumed.calls });
      }
      if (consumed.suffix) {
        state.pending += consumed.suffix;
      }
      continue;
    }

    if (!state.pending) {
      break;
    }

    const start = findToolSegmentStart(state.pending);
    if (start >= 0) {
      const prefix = state.pending.slice(0, start);
      if (prefix) {
        noteText(state, prefix);
        events.push({ type: 'text', text: prefix });
      }
      state.capture = state.pending.slice(start);
      state.pending = '';
      state.capturing = true;
      resetIncrementalToolState(state);
      continue;
    }

    const [safe, hold] = splitSafeContentForToolDetection(state.pending);
    if (!safe) {
      break;
    }
    state.pending = hold;
    noteText(state, safe);
    events.push({ type: 'text', text: safe });
  }
  return events;
}

function flushToolSieve(state, toolNames) {
  if (!state) {
    return [];
  }
  const events = processToolSieveChunk(state, '', toolNames);
  if (state.capturing) {
    const consumed = consumeToolCapture(state, toolNames);
    if (consumed.ready) {
      if (consumed.prefix) {
        noteText(state, consumed.prefix);
        events.push({ type: 'text', text: consumed.prefix });
      }
      if (Array.isArray(consumed.calls) && consumed.calls.length > 0) {
        events.push({ type: 'tool_calls', calls: consumed.calls });
      }
      if (consumed.suffix) {
        noteText(state, consumed.suffix);
        events.push({ type: 'text', text: consumed.suffix });
      }
    } else if (state.capture) {
      noteText(state, state.capture);
      events.push({ type: 'text', text: state.capture });
    }
    state.capture = '';
    state.capturing = false;
    resetIncrementalToolState(state);
  }
  if (state.pending) {
    noteText(state, state.pending);
    events.push({ type: 'text', text: state.pending });
    state.pending = '';
  }
  return events;
}

function splitSafeContentForToolDetection(s) {
  const text = s || '';
  if (!text) {
    return ['', ''];
  }
  const suspiciousStart = findSuspiciousPrefixStart(text);
  if (suspiciousStart < 0) {
    return [text, ''];
  }
  if (suspiciousStart > 0) {
    return [text.slice(0, suspiciousStart), text.slice(suspiciousStart)];
  }
  // If suspicious content starts at the beginning, keep holding until we can
  // either parse a full tool JSON block or reach stream flush.
  return ['', text];
}

function findSuspiciousPrefixStart(s) {
  let start = -1;
  for (const needle of ['{', '[', '```']) {
    const idx = s.lastIndexOf(needle);
    if (idx > start) {
      start = idx;
    }
  }
  return start;
}

function findToolSegmentStart(s) {
  if (!s) {
    return -1;
  }
  const lower = s.toLowerCase();
  let offset = 0;
  // eslint-disable-next-line no-constant-condition
  while (true) {
    const keyRel = lower.indexOf('tool_calls', offset);
    if (keyRel < 0) {
      return -1;
    }
    const keyIdx = keyRel;
    const start = s.slice(0, keyIdx).lastIndexOf('{');
    const candidateStart = start >= 0 ? start : keyIdx;
    if (!insideCodeFence(s.slice(0, candidateStart))) {
      return candidateStart;
    }
    offset = keyIdx + 'tool_calls'.length;
  }
}

function consumeToolCapture(state, toolNames) {
  const captured = state.capture;
  if (!captured) {
    return { ready: false, prefix: '', calls: [], suffix: '' };
  }
  const lower = captured.toLowerCase();
  const keyIdx = lower.indexOf('tool_calls');
  if (keyIdx < 0) {
    return { ready: false, prefix: '', calls: [], suffix: '' };
  }
  const start = captured.slice(0, keyIdx).lastIndexOf('{');
  if (start < 0) {
    return { ready: false, prefix: '', calls: [], suffix: '' };
  }
  const obj = extractJSONObjectFrom(captured, start);
  if (!obj.ok) {
    return { ready: false, prefix: '', calls: [], suffix: '' };
  }
  const prefixPart = captured.slice(0, start);
  const suffixPart = captured.slice(obj.end);
  if (insideCodeFence((state.recentTextTail || '') + prefixPart)) {
    return {
      ready: true,
      prefix: captured,
      calls: [],
      suffix: '',
    };
  }
  const parsed = parseStandaloneToolCalls(captured.slice(start, obj.end), toolNames);
  if (parsed.length === 0) {
    if (state.toolNameSent) {
      return {
        ready: true,
        prefix: prefixPart,
        calls: [],
        suffix: suffixPart,
      };
    }
    return {
      ready: true,
      prefix: captured,
      calls: [],
      suffix: '',
    };
  }
  if (state.toolNameSent) {
    if (parsed.length > 1) {
      return {
        ready: true,
        prefix: prefixPart,
        calls: parsed.slice(1),
        suffix: suffixPart,
      };
    }
    return {
      ready: true,
      prefix: prefixPart,
      calls: [],
      suffix: suffixPart,
    };
  }
  return {
    ready: true,
    prefix: prefixPart,
    calls: parsed,
    suffix: suffixPart,
  };
}

module.exports = {
  processToolSieveChunk,
  flushToolSieve,
};
