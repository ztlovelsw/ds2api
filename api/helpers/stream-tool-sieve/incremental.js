'use strict';

const {
  looksLikeToolExampleContext,
  insideCodeFence,
} = require('./state');
const {
  findObjectFieldValueStart,
  parseJSONStringLiteral,
  skipSpaces,
} = require('./jsonscan');

function buildIncrementalToolDeltas(state) {
  const captured = state.capture || '';
  if (!captured) {
    return [];
  }
  if (looksLikeToolExampleContext(state.recentTextTail)) {
    return [];
  }
  const lower = captured.toLowerCase();
  const keyIdx = lower.indexOf('tool_calls');
  if (keyIdx < 0) {
    return [];
  }
  const start = captured.slice(0, keyIdx).lastIndexOf('{');
  if (start < 0) {
    return [];
  }
  if (insideCodeFence((state.recentTextTail || '') + captured.slice(0, start))) {
    return [];
  }
  const callStart = findFirstToolCallObjectStart(captured, keyIdx);
  if (callStart < 0) {
    return [];
  }

  const deltas = [];
  if (!state.toolName) {
    const name = extractToolCallName(captured, callStart);
    if (!name) {
      return [];
    }
    state.toolName = name;
  }

  if (state.toolArgsStart < 0) {
    const args = findToolCallArgsStart(captured, callStart);
    if (args) {
      state.toolArgsString = Boolean(args.stringMode);
      state.toolArgsStart = state.toolArgsString ? args.start + 1 : args.start;
      state.toolArgsSent = state.toolArgsStart;
    }
  }
  if (!state.toolNameSent) {
    if (state.toolArgsStart < 0) {
      return [];
    }
    state.toolNameSent = true;
    deltas.push({ index: 0, name: state.toolName });
  }
  if (state.toolArgsStart < 0 || state.toolArgsDone) {
    return deltas;
  }
  const progress = scanToolCallArgsProgress(captured, state.toolArgsStart, state.toolArgsString);
  if (!progress) {
    return deltas;
  }
  if (progress.end > state.toolArgsSent) {
    deltas.push({
      index: 0,
      arguments: captured.slice(state.toolArgsSent, progress.end),
    });
    state.toolArgsSent = progress.end;
  }
  if (progress.complete) {
    state.toolArgsDone = true;
  }
  return deltas;
}

function findFirstToolCallObjectStart(text, keyIdx) {
  const arrStart = findToolCallsArrayStart(text, keyIdx);
  if (arrStart < 0) {
    return -1;
  }
  const i = skipSpaces(text, arrStart + 1);
  if (i >= text.length || text[i] !== '{') {
    return -1;
  }
  return i;
}

function findToolCallsArrayStart(text, keyIdx) {
  let i = keyIdx + 'tool_calls'.length;
  while (i < text.length && text[i] !== ':') {
    i += 1;
  }
  if (i >= text.length) {
    return -1;
  }
  i = skipSpaces(text, i + 1);
  if (i >= text.length || text[i] !== '[') {
    return -1;
  }
  return i;
}

function extractToolCallName(text, callStart) {
  let valueStart = findObjectFieldValueStart(text, callStart, ['name']);
  if (valueStart < 0 || text[valueStart] !== '"') {
    const fnStart = findFunctionObjectStart(text, callStart);
    if (fnStart < 0) {
      return '';
    }
    valueStart = findObjectFieldValueStart(text, fnStart, ['name']);
    if (valueStart < 0 || text[valueStart] !== '"') {
      return '';
    }
  }
  const parsed = parseJSONStringLiteral(text, valueStart);
  if (!parsed) {
    return '';
  }
  return parsed.value;
}

function findToolCallArgsStart(text, callStart) {
  const keys = ['input', 'arguments', 'args', 'parameters', 'params'];
  let valueStart = findObjectFieldValueStart(text, callStart, keys);
  if (valueStart < 0) {
    const fnStart = findFunctionObjectStart(text, callStart);
    if (fnStart < 0) {
      return null;
    }
    valueStart = findObjectFieldValueStart(text, fnStart, keys);
    if (valueStart < 0) {
      return null;
    }
  }
  if (valueStart >= text.length) {
    return null;
  }
  const ch = text[valueStart];
  if (ch === '{' || ch === '[') {
    return { start: valueStart, stringMode: false };
  }
  if (ch === '"') {
    return { start: valueStart, stringMode: true };
  }
  return null;
}

function scanToolCallArgsProgress(text, start, stringMode) {
  if (start < 0 || start > text.length) {
    return null;
  }
  if (stringMode) {
    let escaped = false;
    for (let i = start; i < text.length; i += 1) {
      const ch = text[i];
      if (escaped) {
        escaped = false;
        continue;
      }
      if (ch === '\\') {
        escaped = true;
        continue;
      }
      if (ch === '"') {
        return { end: i, complete: true };
      }
    }
    return { end: text.length, complete: false };
  }
  if (start >= text.length || (text[start] !== '{' && text[start] !== '[')) {
    return null;
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
    if (ch === '{' || ch === '[') {
      depth += 1;
      continue;
    }
    if (ch === '}' || ch === ']') {
      depth -= 1;
      if (depth === 0) {
        return { end: i + 1, complete: true };
      }
    }
  }
  return { end: text.length, complete: false };
}

function findFunctionObjectStart(text, callStart) {
  const valueStart = findObjectFieldValueStart(text, callStart, ['function']);
  if (valueStart < 0 || valueStart >= text.length || text[valueStart] !== '{') {
    return -1;
  }
  return valueStart;
}

module.exports = {
  buildIncrementalToolDeltas,
};
