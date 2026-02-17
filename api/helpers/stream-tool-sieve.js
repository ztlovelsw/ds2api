'use strict';

const crypto = require('crypto');
const TOOL_CALL_PATTERN = /\{\s*["']tool_calls["']\s*:\s*\[(.*?)\]\s*\}/s;

function extractToolNames(tools) {
  if (!Array.isArray(tools) || tools.length === 0) {
    return [];
  }
  const out = [];
  for (const t of tools) {
    if (!t || typeof t !== 'object') {
      continue;
    }
    const fn = t.function && typeof t.function === 'object' ? t.function : t;
    const name = toStringSafe(fn.name);
    if (name) {
      out.push(name);
    }
  }
  return out;
}

function createToolSieveState() {
  return {
    pending: '',
    capture: '',
    capturing: false,
  };
}

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
      const consumed = consumeToolCapture(state.capture, toolNames);
      if (!consumed.ready) {
        break;
      }
      state.capture = '';
      state.capturing = false;
      if (consumed.prefix) {
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
        events.push({ type: 'text', text: prefix });
      }
      state.capture = state.pending.slice(start);
      state.pending = '';
      state.capturing = true;
      continue;
    }

    const [safe, hold] = splitSafeContentForToolDetection(state.pending);
    if (!safe) {
      break;
    }
    state.pending = hold;
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
    const consumed = consumeToolCapture(state.capture, toolNames);
    if (consumed.ready) {
      if (consumed.prefix) {
        events.push({ type: 'text', text: consumed.prefix });
      }
      if (Array.isArray(consumed.calls) && consumed.calls.length > 0) {
        events.push({ type: 'tool_calls', calls: consumed.calls });
      }
      if (consumed.suffix) {
        events.push({ type: 'text', text: consumed.suffix });
      }
    } else if (state.capture) {
      // Incomplete captured tool JSON at stream end: suppress raw capture.
    }
    state.capture = '';
    state.capturing = false;
  }
  if (state.pending) {
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
  const keyIdx = lower.indexOf('tool_calls');
  if (keyIdx < 0) {
    return -1;
  }
  const start = s.slice(0, keyIdx).lastIndexOf('{');
  return start >= 0 ? start : keyIdx;
}

function consumeToolCapture(captured, toolNames) {
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
  const parsed = parseToolCalls(captured.slice(start, obj.end), toolNames);
  if (parsed.length === 0) {
    // `tool_calls` key exists but strict JSON parse failed.
    // Drop the captured object body to avoid leaking raw tool JSON.
    return {
      ready: true,
      prefix: captured.slice(0, start),
      calls: [],
      suffix: captured.slice(obj.end),
    };
  }
  return {
    ready: true,
    prefix: captured.slice(0, start),
    calls: parsed,
    suffix: captured.slice(obj.end),
  };
}

function extractJSONObjectFrom(text, start) {
  if (!text || start < 0 || start >= text.length || text[start] !== '{') {
    return { ok: false, end: 0 };
  }
  let depth = 0;
  let quote = '';
  let escaped = false;
  for (let i = start; i < text.length; i += 1) {
    const ch = text[i];
    if (quote) {
      if (escaped) {
        escaped = false;
        continue;
      }
      if (ch === '\\') {
        escaped = true;
        continue;
      }
      if (ch === quote) {
        quote = '';
      }
      continue;
    }
    if (ch === '"' || ch === "'") {
      quote = ch;
      continue;
    }
    if (ch === '{') {
      depth += 1;
      continue;
    }
    if (ch === '}') {
      depth -= 1;
      if (depth === 0) {
        return { ok: true, end: i + 1 };
      }
    }
  }
  return { ok: false, end: 0 };
}

function parseToolCalls(text, toolNames) {
  if (!toStringSafe(text)) {
    return [];
  }
  const candidates = buildToolCallCandidates(text);
  let parsed = [];
  for (const c of candidates) {
    parsed = parseToolCallsPayload(c);
    if (parsed.length > 0) {
      break;
    }
  }
  if (parsed.length === 0) {
    return [];
  }
  const allowed = new Set((toolNames || []).filter(Boolean));
  const out = [];
  for (const tc of parsed) {
    if (!tc || !tc.name) {
      continue;
    }
    if (allowed.size > 0 && !allowed.has(tc.name)) {
      continue;
    }
    out.push({ name: tc.name, input: tc.input || {} });
  }
  if (out.length === 0 && parsed.length > 0) {
    for (const tc of parsed) {
      if (!tc || !tc.name) {
        continue;
      }
      out.push({ name: tc.name, input: tc.input || {} });
    }
  }
  return out;
}

function buildToolCallCandidates(text) {
  const trimmed = toStringSafe(text);
  const candidates = [trimmed];
  const fenced = trimmed.match(/```(?:json)?\s*([\s\S]*?)\s*```/gi) || [];
  for (const block of fenced) {
    const m = block.match(/```(?:json)?\s*([\s\S]*?)\s*```/i);
    if (m && m[1]) {
      candidates.push(toStringSafe(m[1]));
    }
  }
  for (const candidate of extractToolCallObjects(trimmed)) {
    candidates.push(toStringSafe(candidate));
  }
  const first = trimmed.indexOf('{');
  const last = trimmed.lastIndexOf('}');
  if (first >= 0 && last > first) {
    candidates.push(toStringSafe(trimmed.slice(first, last + 1)));
  }
  const m = trimmed.match(TOOL_CALL_PATTERN);
  if (m && m[1]) {
    candidates.push(`{"tool_calls":[${m[1]}]}`);
  }
  return [...new Set(candidates.filter(Boolean))];
}

function extractToolCallObjects(text) {
  const raw = toStringSafe(text);
  if (!raw) {
    return [];
  }
  const lower = raw.toLowerCase();
  const out = [];
  let offset = 0;
  // eslint-disable-next-line no-constant-condition
  while (true) {
    let idx = lower.indexOf('tool_calls', offset);
    if (idx < 0) {
      break;
    }
    let start = raw.slice(0, idx).lastIndexOf('{');
    while (start >= 0) {
      const obj = extractJSONObjectFrom(raw, start);
      if (obj.ok) {
        out.push(raw.slice(start, obj.end).trim());
        offset = obj.end;
        idx = -1;
        break;
      }
      start = raw.slice(0, start).lastIndexOf('{');
    }
    if (idx >= 0) {
      offset = idx + 'tool_calls'.length;
    }
  }
  return out;
}

function parseToolCallsPayload(payload) {
  let decoded;
  try {
    decoded = JSON.parse(payload);
  } catch (_err) {
    return [];
  }
  if (Array.isArray(decoded)) {
    return parseToolCallList(decoded);
  }
  if (!decoded || typeof decoded !== 'object') {
    return [];
  }
  if (decoded.tool_calls) {
    return parseToolCallList(decoded.tool_calls);
  }
  const one = parseToolCallItem(decoded);
  return one ? [one] : [];
}

function parseToolCallList(v) {
  if (!Array.isArray(v)) {
    return [];
  }
  const out = [];
  for (const item of v) {
    if (!item || typeof item !== 'object') {
      continue;
    }
    const one = parseToolCallItem(item);
    if (one) {
      out.push(one);
    }
  }
  return out;
}

function parseToolCallItem(m) {
  let name = toStringSafe(m.name);
  let inputRaw = m.input;
  let hasInput = Object.prototype.hasOwnProperty.call(m, 'input');
  const fn = m.function && typeof m.function === 'object' ? m.function : null;
  if (fn) {
    if (!name) {
      name = toStringSafe(fn.name);
    }
    if (!hasInput && Object.prototype.hasOwnProperty.call(fn, 'arguments')) {
      inputRaw = fn.arguments;
      hasInput = true;
    }
  }
  if (!hasInput) {
    for (const k of ['arguments', 'args', 'parameters', 'params']) {
      if (Object.prototype.hasOwnProperty.call(m, k)) {
        inputRaw = m[k];
        hasInput = true;
        break;
      }
    }
  }
  if (!name) {
    return null;
  }
  return {
    name,
    input: parseToolCallInput(inputRaw),
  };
}

function parseToolCallInput(v) {
  if (v == null) {
    return {};
  }
  if (typeof v === 'string') {
    const raw = toStringSafe(v);
    if (!raw) {
      return {};
    }
    try {
      const parsed = JSON.parse(raw);
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        return parsed;
      }
    } catch (_err) {
      return { _raw: raw };
    }
    return {};
  }
  if (typeof v === 'object' && !Array.isArray(v)) {
    return v;
  }
  try {
    const parsed = JSON.parse(JSON.stringify(v));
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed;
    }
  } catch (_err) {
    return {};
  }
  return {};
}

function formatOpenAIStreamToolCalls(calls) {
  if (!Array.isArray(calls) || calls.length === 0) {
    return [];
  }
  return calls.map((c, idx) => ({
    index: idx,
    id: `call_${newCallID()}`,
    type: 'function',
    function: {
      name: c.name,
      arguments: JSON.stringify(c.input || {}),
    },
  }));
}

function newCallID() {
  if (typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID().replace(/-/g, '');
  }
  return `${Date.now()}${Math.floor(Math.random() * 1e9)}`;
}

function toStringSafe(v) {
  if (typeof v === 'string') {
    return v.trim();
  }
  if (Array.isArray(v)) {
    return toStringSafe(v[0]);
  }
  if (v == null) {
    return '';
  }
  return String(v).trim();
}

module.exports = {
  extractToolNames,
  createToolSieveState,
  processToolSieveChunk,
  flushToolSieve,
  parseToolCalls,
  formatOpenAIStreamToolCalls,
};
