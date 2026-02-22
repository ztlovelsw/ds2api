'use strict';

function buildUsage(prompt, thinking, output) {
  const promptTokens = estimateTokens(prompt);
  const reasoningTokens = estimateTokens(thinking);
  const completionTokens = estimateTokens(output);
  return {
    prompt_tokens: promptTokens,
    completion_tokens: reasoningTokens + completionTokens,
    total_tokens: promptTokens + reasoningTokens + completionTokens,
    completion_tokens_details: {
      reasoning_tokens: reasoningTokens,
    },
  };
}

function estimateTokens(text) {
  const t = asString(text);
  if (!t) {
    return 0;
  }
  let asciiChars = 0;
  let nonASCIIChars = 0;
  for (const ch of Array.from(t)) {
    if (ch.charCodeAt(0) < 128) {
      asciiChars += 1;
    } else {
      nonASCIIChars += 1;
    }
  }
  const n = Math.floor(asciiChars / 4) + Math.floor((nonASCIIChars * 10 + 7) / 13);
  return n < 1 ? 1 : n;
}

function asString(v) {
  if (typeof v === 'string') {
    return v.trim();
  }
  if (Array.isArray(v)) {
    return asString(v[0]);
  }
  if (v == null) {
    return '';
  }
  return String(v).trim();
}

module.exports = {
  buildUsage,
  estimateTokens,
};
