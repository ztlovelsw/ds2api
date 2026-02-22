'use strict';

const crypto = require('crypto');

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

module.exports = {
  formatOpenAIStreamToolCalls,
};
