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

function diffScopedRuleIds(filePath, code) {
  const script = `
    const { ESLint } = require('eslint');
    let code = '';
    process.stdin.on('data', (c) => (code += c));
    process.stdin.on('end', async () => {
      const [r] = await new ESLint({ cwd: process.argv[1] }).lintText(code, { filePath: process.argv[2] });
      console.log(JSON.stringify(r.messages.map((m) => m.ruleId)));
    });`;
  const out = require('node:child_process').execFileSync(
    process.execPath,
    ['-e', script, __dirname, filePath],
    { input: code, env: { ...process.env, ESLINT_DIFF_SCOPED: '1' }, encoding: 'utf8' },
  );
  return JSON.parse(out);
}

describe('mobile diff-scoped style rules', () => {
  const path = 'src/shared/errors.ts';

  it('allows a data object key and destructured name', () => {
    const rules = diffScopedRuleIds(
      path,
      'const q = 1;\nexport const a = { data: q };\nexport const c = ({ data: d }: { data: number }) => d;\n',
    );
    assert.ok(!rules.includes('id-match') && !rules.includes('id-denylist'));
  });

  it('still flags a data variable binding', () => {
    assert.ok(diffScopedRuleIds(path, 'export const data = 1;\n').includes('id-match'));
  });

  it('exempts useDiscoverSearch from the function line cap only', () => {
    const body = Array.from({ length: 12 }, (_, i) => `  const v${i} = ${i};`).join('\n');
    const code = `export function f() {\n${body}\n  return v0;\n}\n`;
    const hook = 'src/features/discover/hooks/useDiscoverSearch.ts';
    assert.ok(!diffScopedRuleIds(hook, code).includes('max-lines-per-function'));
    assert.ok(diffScopedRuleIds(path, code).includes('max-lines-per-function'));
  });
});
