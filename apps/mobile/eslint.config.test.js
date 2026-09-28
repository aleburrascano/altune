const { ESLint } = require('eslint');
const { describe, it } = require('node:test');
const assert = require('node:assert');

const eslint = new ESLint({ cwd: __dirname });

async function noCommentsErrors(filePath, code) {
  const [result] = await eslint.lintText(code, { filePath });
  return result.messages.filter((m) => m.ruleId === 'local/no-comments' && m.severity === 2);
}

describe('mobile eslint config bans comments in every file', () => {
  for (const file of [
    '__tests__/rootLayout.integration.test.tsx',
    'jest/setup-env.js',
    'scripts/lint-changed-lines.mjs',
    'src/features/library/__tests__/gridColumns.test.ts',
  ]) {
    it(`reports one error for a comment in ${file}`, async () => {
      assert.strictEqual((await noCommentsErrors(file, 'export const a = 1;\n// x\n')).length, 1);
    });
  }

  it('reports nothing for a string that only looks like a comment', async () => {
    const errors = await noCommentsErrors('src/shared/errors.ts', "export const u = 'https://x.dev';\n");
    assert.strictEqual(errors.length, 0);
  });

  it('still reports a comment under a whole-file eslint-disable', async () => {
    const errors = await noCommentsErrors('src/shared/errors.ts', '/* eslint-disable */\nexport const a = 1;\n');
    assert.strictEqual(errors.length, 1);
  });
});
