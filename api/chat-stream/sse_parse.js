'use strict';

const {
  SKIP_PATTERNS,
  SKIP_EXACT_PATHS,
} = require('../shared/deepseek-constants');

function parseChunkForContent(chunk, thinkingEnabled, currentType) {
  if (!chunk || typeof chunk !== 'object' || !Object.prototype.hasOwnProperty.call(chunk, 'v')) {
    return { parts: [], finished: false, newType: currentType };
  }
  const pathValue = asString(chunk.p);
  if (shouldSkipPath(pathValue)) {
    return { parts: [], finished: false, newType: currentType };
  }
  if (pathValue === 'response/status' && asString(chunk.v) === 'FINISHED') {
    return { parts: [], finished: true, newType: currentType };
  }

  let newType = currentType;
  const parts = [];

  if (pathValue === 'response/fragments' && asString(chunk.o).toUpperCase() === 'APPEND' && Array.isArray(chunk.v)) {
    for (const frag of chunk.v) {
      if (!frag || typeof frag !== 'object') {
        continue;
      }
      const fragType = asString(frag.type).toUpperCase();
      const content = asString(frag.content);
      if (!content) {
        continue;
      }
      if (fragType === 'THINK' || fragType === 'THINKING') {
        newType = 'thinking';
        parts.push({ text: content, type: 'thinking' });
      } else if (fragType === 'RESPONSE') {
        newType = 'text';
        parts.push({ text: content, type: 'text' });
      } else {
        parts.push({ text: content, type: 'text' });
      }
    }
  }

  if (pathValue === 'response' && Array.isArray(chunk.v)) {
    for (const item of chunk.v) {
      if (!item || typeof item !== 'object') {
        continue;
      }
      if (item.p === 'fragments' && item.o === 'APPEND' && Array.isArray(item.v)) {
        for (const frag of item.v) {
          const fragType = asString(frag && frag.type).toUpperCase();
          if (fragType === 'THINK' || fragType === 'THINKING') {
            newType = 'thinking';
          } else if (fragType === 'RESPONSE') {
            newType = 'text';
          }
        }
      }
    }
  }

  let partType = 'text';
  if (pathValue === 'response/thinking_content') {
    partType = 'thinking';
  } else if (pathValue === 'response/content') {
    partType = 'text';
  } else if (pathValue.includes('response/fragments') && pathValue.includes('/content')) {
    partType = newType;
  } else if (!pathValue && thinkingEnabled) {
    partType = newType;
  }

  const val = chunk.v;
  if (typeof val === 'string') {
    if (val === 'FINISHED' && (!pathValue || pathValue === 'status')) {
      return { parts: [], finished: true, newType };
    }
    if (val) {
      parts.push({ text: val, type: partType });
    }
    return { parts, finished: false, newType };
  }

  if (Array.isArray(val)) {
    const extracted = extractContentRecursive(val, partType);
    if (extracted.finished) {
      return { parts: [], finished: true, newType };
    }
    parts.push(...extracted.parts);
    return { parts, finished: false, newType };
  }

  if (val && typeof val === 'object') {
    const resp = val.response && typeof val.response === 'object' ? val.response : val;
    if (Array.isArray(resp.fragments)) {
      for (const frag of resp.fragments) {
        if (!frag || typeof frag !== 'object') {
          continue;
        }
        const content = asString(frag.content);
        if (!content) {
          continue;
        }
        const t = asString(frag.type).toUpperCase();
        if (t === 'THINK' || t === 'THINKING') {
          newType = 'thinking';
          parts.push({ text: content, type: 'thinking' });
        } else if (t === 'RESPONSE') {
          newType = 'text';
          parts.push({ text: content, type: 'text' });
        } else {
          parts.push({ text: content, type: partType });
        }
      }
    }
  }
  return { parts, finished: false, newType };
}

function extractContentRecursive(items, defaultType) {
  const parts = [];
  for (const it of items) {
    if (!it || typeof it !== 'object') {
      continue;
    }
    if (!Object.prototype.hasOwnProperty.call(it, 'v')) {
      continue;
    }
    const itemPath = asString(it.p);
    const itemV = it.v;
    if (itemPath === 'status' && asString(itemV) === 'FINISHED') {
      return { parts: [], finished: true };
    }
    if (shouldSkipPath(itemPath)) {
      continue;
    }
    const content = asString(it.content);
    if (content) {
      const typeName = asString(it.type).toUpperCase();
      if (typeName === 'THINK' || typeName === 'THINKING') {
        parts.push({ text: content, type: 'thinking' });
      } else if (typeName === 'RESPONSE') {
        parts.push({ text: content, type: 'text' });
      } else {
        parts.push({ text: content, type: defaultType });
      }
      continue;
    }

    let partType = defaultType;
    if (itemPath.includes('thinking')) {
      partType = 'thinking';
    } else if (itemPath.includes('content') || itemPath === 'response' || itemPath === 'fragments') {
      partType = 'text';
    }

    if (typeof itemV === 'string') {
      if (itemV && itemV !== 'FINISHED') {
        parts.push({ text: itemV, type: partType });
      }
      continue;
    }

    if (!Array.isArray(itemV)) {
      continue;
    }
    for (const inner of itemV) {
      if (typeof inner === 'string') {
        if (inner) {
          parts.push({ text: inner, type: partType });
        }
        continue;
      }
      if (!inner || typeof inner !== 'object') {
        continue;
      }
      const ct = asString(inner.content);
      if (!ct) {
        continue;
      }
      const typeName = asString(inner.type).toUpperCase();
      if (typeName === 'THINK' || typeName === 'THINKING') {
        parts.push({ text: ct, type: 'thinking' });
      } else if (typeName === 'RESPONSE') {
        parts.push({ text: ct, type: 'text' });
      } else {
        parts.push({ text: ct, type: partType });
      }
    }
  }
  return { parts, finished: false };
}

function shouldSkipPath(pathValue) {
  if (SKIP_EXACT_PATHS.has(pathValue)) {
    return true;
  }
  for (const p of SKIP_PATTERNS) {
    if (pathValue.includes(p)) {
      return true;
    }
  }
  return false;
}

function isCitation(text) {
  return asString(text).trim().startsWith('[citation:');
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
  parseChunkForContent,
  extractContentRecursive,
  shouldSkipPath,
  isCitation,
};
