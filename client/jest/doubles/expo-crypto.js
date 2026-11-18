const { randomUUID: nodeRandomUUID } = require('node:crypto');

const queued = [];

function randomUUID() {
  return queued.length > 0 ? queued.shift() : nodeRandomUUID();
}

const __crypto = {
  nextUuid(id) {
    queued.push(id);
  },
  reset() {
    queued.length = 0;
  },
};

module.exports = { randomUUID, __crypto };
