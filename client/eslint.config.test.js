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
    const hook = 'src/features/discovery/hooks/useDiscoverSearch.ts';
    assert.ok(!diffScopedRuleIds(hook, code).includes('max-lines-per-function'));
    assert.ok(diffScopedRuleIds(path, code).includes('max-lines-per-function'));
  });
});

describe('browser globals', () => {
  const untyped = new ESLint({
    cwd: __dirname,
    overrideConfig: {
      languageOptions: { parserOptions: { projectService: false } },
      rules: Object.fromEntries(
        [
          'no-floating-promises',
          'no-misused-promises',
          'await-thenable',
          'no-misused-spread',
          'use-unknown-in-catch-callback-variable',
          'no-unnecessary-type-assertion',
        ].map((name) => [`@typescript-eslint/${name}`, 'off']),
      ),
    },
  });

  async function globalErrors(filePath, code) {
    const [result] = await untyped.lintText(code, { filePath });
    return result.messages.filter((m) => m.ruleId === 'no-restricted-globals');
  }

  const alertCode = "window.alert('x');\n";

  it('flags window in a shared file', async () => {
    assert.strictEqual((await globalErrors('src/shared/x.ts', alertCode)).length, 1);
  });

  it('flags typeof document in a feature hook', async () => {
    const errors = await globalErrors('src/features/library/hooks/y.ts', 'export const t = typeof document;\n');
    assert.strictEqual(errors.length, 1);
  });

  for (const file of ['src/shared/x.web.ts', 'src/shared/x.web.tsx', 'src/features/playback/web/y.ts']) {
    it(`allows window in ${file}`, async () => {
      assert.strictEqual((await globalErrors(file, alertCode)).length, 0);
    });
  }

  it('allows window in a test file', async () => {
    assert.strictEqual((await globalErrors('src/shared/__tests__/x.test.ts', alertCode)).length, 0);
  });

  for (const file of ['src/shared/webby.ts', 'src/shared/website/x.ts', 'src/shared/x.webview.ts']) {
    it(`refuses window in look-alike ${file}`, async () => {
      assert.strictEqual((await globalErrors(file, alertCode)).length, 1);
    });
  }

  it('allows a parameter named navigator', async () => {
    const code = 'export function f(navigator: { back(): void }) { navigator.back(); }\n';
    assert.strictEqual((await globalErrors('src/shared/x.ts', code)).length, 0);
  });

  it('finds no offender anywhere under src', async () => {
    const results = await eslint.lintFiles(['src/**/*.{ts,tsx}']);
    const offenders = results.flatMap((r) => r.messages.filter((m) => m.ruleId === 'no-restricted-globals'));
    assert.strictEqual(offenders.length, 0);
  });
});
