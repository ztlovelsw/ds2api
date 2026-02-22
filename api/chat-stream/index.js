'use strict';

const {
  writeOpenAIError,
} = require('./error_shape');
const {
  parseChunkForContent,
  extractContentRecursive,
  shouldSkipPath,
} = require('./sse_parse');
const {
  resolveToolcallPolicy,
  normalizePreparedToolNames,
  boolDefaultTrue,
} = require('./toolcall_policy');
const {
  estimateTokens,
} = require('./token_usage');
const {
  setCorsHeaders,
  readRawBody,
  asString,
} = require('./http_internal');
const {
  proxyToGo,
} = require('./proxy_go');
const {
  handleVercelStream,
} = require('./vercel_stream');

async function handler(req, res) {
  setCorsHeaders(res);
  if (req.method === 'OPTIONS') {
    res.statusCode = 204;
    res.end();
    return;
  }
  if (req.method !== 'POST') {
    writeOpenAIError(res, 405, 'method not allowed');
    return;
  }

  const rawBody = await readRawBody(req);

  // Hard guard: only use Node data path for streaming on Vercel runtime.
  // Any non-Vercel runtime always falls back to Go for full behavior parity.
  if (!isVercelRuntime()) {
    await proxyToGo(req, res, rawBody);
    return;
  }

  let payload;
  try {
    payload = JSON.parse(rawBody.toString('utf8') || '{}');
  } catch (_err) {
    writeOpenAIError(res, 400, 'invalid json');
    return;
  }

  // Keep all non-stream behavior on Go side to avoid compatibility regressions.
  if (!toBool(payload.stream)) {
    await proxyToGo(req, res, rawBody);
    return;
  }

  await handleVercelStream(req, res, rawBody, payload);
}

function toBool(v) {
  return v === true;
}

function isVercelRuntime() {
  return asString(process.env.VERCEL) !== '' || asString(process.env.NOW_REGION) !== '';
}

module.exports = handler;

module.exports.__test = {
  parseChunkForContent,
  extractContentRecursive,
  shouldSkipPath,
  asString,
  resolveToolcallPolicy,
  normalizePreparedToolNames,
  boolDefaultTrue,
  estimateTokens,
};
