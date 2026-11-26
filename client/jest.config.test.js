const { describe, it } = require('node:test');
const assert = require('node:assert');

const config = require('./jest.config.js');

describe('jest worker memory', () => {
  it('caps the workers once, for both projects, so earlyoom on the shared CI box does not kill one', () => {
    assert.strictEqual(config.maxWorkers, 2);
    assert.strictEqual(config.workerIdleMemoryLimit, '1G');
    for (const project of config.projects) {
      assert.strictEqual(project.maxWorkers, undefined);
      assert.strictEqual(project.workerIdleMemoryLimit, undefined);
    }
  });
});
