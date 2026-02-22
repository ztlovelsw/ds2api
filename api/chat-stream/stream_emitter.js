'use strict';

function createChatCompletionEmitter({ res, sessionID, created, model, isClosed }) {
  let firstChunkSent = false;

  const sendFrame = (obj) => {
    if (isClosed() || res.writableEnded || res.destroyed) {
      return;
    }
    res.write(`data: ${JSON.stringify(obj)}\n\n`);
    if (typeof res.flush === 'function') {
      res.flush();
    }
  };

  const sendDeltaFrame = (delta) => {
    const payloadDelta = { ...delta };
    if (!firstChunkSent) {
      payloadDelta.role = 'assistant';
      firstChunkSent = true;
    }
    sendFrame({
      id: sessionID,
      object: 'chat.completion.chunk',
      created,
      model,
      choices: [{ delta: payloadDelta, index: 0 }],
    });
  };

  return {
    sendFrame,
    sendDeltaFrame,
  };
}

module.exports = {
  createChatCompletionEmitter,
};
