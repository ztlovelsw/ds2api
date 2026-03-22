'use strict';

const TOOL_SEGMENT_KEYWORDS = [
  'tool_calls',
  '"function"',
  'function.name:',
  '[tool_call_history]',
  '[tool_result_history]',
];

function earliestKeywordIndex(text, keywords = TOOL_SEGMENT_KEYWORDS, offset = 0) {
  if (!text) {
    return { index: -1, keyword: '' };
  }
  let index = -1;
  let keyword = '';
  for (const kw of keywords) {
    const candidate = text.indexOf(kw, offset);
    if (candidate >= 0 && (index < 0 || candidate < index)) {
      index = candidate;
      keyword = kw;
    }
  }
  return { index, keyword };
}

module.exports = {
  TOOL_SEGMENT_KEYWORDS,
  earliestKeywordIndex,
};
