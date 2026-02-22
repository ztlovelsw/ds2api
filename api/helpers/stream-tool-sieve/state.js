'use strict';

const TOOL_SIEVE_CAPTURE_LIMIT = 8 * 1024;
const TOOL_SIEVE_CONTEXT_TAIL_LIMIT = 256;

function createToolSieveState() {
  return {
    pending: '',
    capture: '',
    capturing: false,
    recentTextTail: '',
    toolNameSent: false,
    toolName: '',
    toolArgsStart: -1,
    toolArgsSent: -1,
    toolArgsString: false,
    toolArgsDone: false,
  };
}

function resetIncrementalToolState(state) {
  state.toolNameSent = false;
  state.toolName = '';
  state.toolArgsStart = -1;
  state.toolArgsSent = -1;
  state.toolArgsString = false;
  state.toolArgsDone = false;
}

function noteText(state, text) {
  if (!state || !hasMeaningfulText(text)) {
    return;
  }
  state.recentTextTail = appendTail(state.recentTextTail, text, TOOL_SIEVE_CONTEXT_TAIL_LIMIT);
}

function appendTail(prev, next, max) {
  const left = typeof prev === 'string' ? prev : '';
  const right = typeof next === 'string' ? next : '';
  if (!Number.isFinite(max) || max <= 0) {
    return '';
  }
  const combined = left + right;
  if (combined.length <= max) {
    return combined;
  }
  return combined.slice(combined.length - max);
}

function looksLikeToolExampleContext(text) {
  return insideCodeFence(text);
}

function insideCodeFence(text) {
  const t = typeof text === 'string' ? text : '';
  if (!t) {
    return false;
  }
  const ticks = (t.match(/```/g) || []).length;
  return ticks % 2 === 1;
}

function hasMeaningfulText(text) {
  return toStringSafe(text) !== '';
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
  TOOL_SIEVE_CAPTURE_LIMIT,
  TOOL_SIEVE_CONTEXT_TAIL_LIMIT,
  createToolSieveState,
  resetIncrementalToolState,
  noteText,
  appendTail,
  looksLikeToolExampleContext,
  insideCodeFence,
  hasMeaningfulText,
  toStringSafe,
};
