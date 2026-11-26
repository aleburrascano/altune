const { ESLint } = require('eslint');
const { describe, it } = require('node:test');
const assert = require('node:assert');

const eslint = new ESLint({ cwd: __dirname });

const typeAwareRulesOff = Object.fromEntries(
  Object.entries(require('@typescript-eslint/eslint-plugin').rules)
    .filter(([, rule]) => rule.meta.docs.requiresTypeChecking === true)
    .map(([name]) => [`@typescript-eslint/${name}`, 'off']),
);

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
    const errors = await noCommentsErrors(
      'src/shared/failure/core/errors.ts',
      "export const u = 'https://x.dev';\n",
    );
    assert.strictEqual(errors.length, 0);
  });

  it('still reports a comment under a whole-file eslint-disable', async () => {
    const errors = await noCommentsErrors(
      'src/shared/failure/core/errors.ts',
      '/* eslint-disable */\nexport const a = 1;\n',
    );
    assert.strictEqual(errors.length, 1);
  });
});

function diffScopedRuleIds(filePath, code, extraEnv = {}) {
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
    {
      input: code,
      env: { ...process.env, ESLINT_DIFF_SCOPED: '1', ...extraEnv },
      encoding: 'utf8',
    },
  );
  return JSON.parse(out);
}

describe('mobile eslint without type info (commit stage)', () => {
  const path = 'src/shared/failure/core/errors.ts';
  const code = 'export const data = 1;\nPromise.resolve(data);\n';

  it('drops type-aware rules but keeps untyped ones', () => {
    const rules = diffScopedRuleIds(path, code, { ESLINT_WITHOUT_TYPES: '1' });
    assert.ok(!rules.includes('@typescript-eslint/no-floating-promises'));
    assert.ok(rules.includes('id-match'));
  });

  it('reports the type-aware rule once type info is on', () => {
    const rules = diffScopedRuleIds(path, code, { ESLINT_WITHOUT_TYPES: '0' });
    assert.ok(rules.includes('@typescript-eslint/no-floating-promises'));
  });
});

describe('mobile diff-scoped style rules', () => {
  const path = 'src/shared/failure/core/errors.ts';

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

  it('leaves function length and branching to review, not a number', () => {
    const body = Array.from({ length: 12 }, (_, i) => `  const v${i} = ${i};`).join('\n');
    const branches = Array.from({ length: 12 }, (_, i) => `  if (v0 > ${i}) return ${i};`).join(
      '\n',
    );
    const code = `export function f() {\n${body}\n${branches}\n  return v0;\n}\n`;
    const rules = diffScopedRuleIds(path, code);
    assert.ok(!rules.includes('max-lines-per-function') && !rules.includes('complexity'));
  });
});

describe('browser globals', () => {
  const untyped = new ESLint({
    cwd: __dirname,
    overrideConfig: {
      languageOptions: { parserOptions: { projectService: false } },
      rules: typeAwareRulesOff,
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
    const errors = await globalErrors(
      'src/features/library/hooks/y.ts',
      'export const t = typeof document;\n',
    );
    assert.strictEqual(errors.length, 1);
  });

  for (const file of [
    'src/shared/x.web.ts',
    'src/shared/x.web.tsx',
    'src/features/player/web/y.ts',
  ]) {
    it(`allows window in ${file}`, async () => {
      assert.strictEqual((await globalErrors(file, alertCode)).length, 0);
    });
  }

  it('allows window in a test file', async () => {
    assert.strictEqual((await globalErrors('src/shared/__tests__/x.test.ts', alertCode)).length, 0);
  });

  for (const file of [
    'src/shared/webby.ts',
    'src/shared/website/x.ts',
    'src/shared/x.webview.ts',
  ]) {
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
    const offenders = results.flatMap((r) =>
      r.messages.filter((m) => m.ruleId === 'no-restricted-globals'),
    );
    assert.strictEqual(offenders.length, 0);
  });
});

describe('mobile eslint config bans relative parent imports', () => {
  async function parentImportErrors(filePath, code) {
    const [result] = await eslint.lintText(code, { filePath });
    return result.messages.filter(
      (m) => m.ruleId === 'local/no-parent-relative-imports' && m.severity === 2,
    );
  }

  for (const file of [
    'src/shared/failure/core/errors.ts',
    'src/features/library/__tests__/gridColumns.test.ts',
    '__tests__/rootLayout.integration.test.tsx',
  ]) {
    it(`reports a relative parent import in ${file}`, async () => {
      assert.strictEqual(
        (await parentImportErrors(file, "import x from '../y';\nexport const a = x;\n")).length,
        1,
      );
    });

    it(`reports jest.mock of a relative parent path in ${file}`, async () => {
      assert.strictEqual((await parentImportErrors(file, "jest.mock('../y');\n")).length, 1);
    });
  }

  it('allows an alias and a sibling import', async () => {
    const code =
      "import a from '@shared/failure/errors';\nimport b from './b';\nexport const c = [a, b];\n";
    assert.strictEqual((await parentImportErrors('src/shared/failure/x.ts', code)).length, 0);
  });
});

describe('mobile eslint config makes top-level functions declarations', () => {
  async function arrowConstErrors(filePath, code) {
    const [result] = await eslint.lintText(code, { filePath });
    return result.messages.filter(
      (m) => m.ruleId === 'local/top-level-function-declarations' && m.severity === 2,
    );
  }

  it('reports a top-level arrow const in src', async () => {
    const errors = await arrowConstErrors(
      'src/shared/failure/core/errors.ts',
      'export const f = () => 1;\n',
    );
    assert.strictEqual(errors.length, 1);
  });

  it('allows a nested arrow const inside a declaration', async () => {
    const code = 'export function f() {\n  const g = () => 1;\n  return g();\n}\n';
    assert.strictEqual(
      (await arrowConstErrors('src/shared/failure/core/errors.ts', code)).length,
      0,
    );
  });

  it('ignores test files', async () => {
    const file = 'src/features/library/__tests__/gridColumns.test.ts';
    assert.strictEqual((await arrowConstErrors(file, 'export const f = () => 1;\n')).length, 0);
  });
});

describe('mobile eslint config keeps raw style numbers out of StyleSheet.create', () => {
  async function rawStyleErrors(filePath, code) {
    const [result] = await eslint.lintText(code, { filePath });
    return result.messages.filter(
      (m) => m.ruleId === 'local/no-raw-style-numbers' && m.severity === 2,
    );
  }
  it('reports a raw number in a stylesheet in src', async () => {
    const code =
      "import { StyleSheet } from 'react-native';\nexport const styles = StyleSheet.create({ a: { width: 44 } });\n";
    assert.strictEqual(
      (await rawStyleErrors('src/shared/ui/primitives/TextField.tsx', code)).length,
      1,
    );
  });
  it('allows 0, 1 and flex', async () => {
    const code =
      "import { StyleSheet } from 'react-native';\nexport const styles = StyleSheet.create({ a: { flex: 3, margin: 0, opacity: 1 } });\n";
    assert.strictEqual(
      (await rawStyleErrors('src/shared/ui/primitives/TextField.tsx', code)).length,
      0,
    );
  });
  it('leaves the theme folder alone', async () => {
    const code =
      "import { StyleSheet } from 'react-native';\nexport const styles = StyleSheet.create({ a: { width: 44 } });\n";
    assert.strictEqual(
      (await rawStyleErrors('src/shared/ui/view/theme/ThemeProvider.tsx', code)).length,
      0,
    );
  });
});

describe('module rules point at client/src/README.md', () => {
  const untyped = new ESLint({
    cwd: __dirname,
    overrideConfig: {
      languageOptions: { parserOptions: { projectService: false } },
      rules: typeAwareRulesOff,
    },
  });

  async function errors(ruleId, filePath, code) {
    const [result] = await untyped.lintText(code, { filePath });
    return result.messages.filter((m) => m.ruleId === ruleId && m.severity === 2);
  }

  const cases = [
    [
      'no-restricted-imports',
      'src/shared/math/core/x.ts',
      "import { useState } from 'react';\nexport const s = useState;\n",
      1,
    ],
    [
      'no-restricted-imports',
      'src/shared/math/core/x.ts',
      "import type { ReactNode } from 'react';\nexport type N = ReactNode;\n",
      1,
    ],
    [
      'no-restricted-imports',
      'src/shared/math/core/x.ts',
      "import { create } from 'zustand';\nimport { trackId } from '@shared/catalog';\nexport const c = [create, trackId];\n",
      0,
    ],
    [
      'no-restricted-imports',
      'src/shared/math/core/x.ts',
      "import { useQuery } from '@tanstack/react-query';\nexport const q = useQuery;\n",
      0,
    ],
    [
      'no-restricted-imports',
      'src/features/player/shell/x.ts',
      "import * as fs from 'expo-file-system';\nexport const f = fs;\n",
      1,
    ],
    [
      'no-restricted-imports',
      'src/features/player/ports/x.ts',
      "import { Platform } from 'react-native';\nexport const p = Platform;\n",
      1,
    ],
    [
      'no-restricted-imports',
      'src/features/player/view/X.tsx',
      "import { useRouter } from 'expo-router';\nexport const r = useRouter;\n",
      1,
    ],
    [
      'no-restricted-imports',
      'src/features/player/view/X.tsx',
      "import { View } from 'react-native';\nimport { Play } from 'lucide-react-native';\nimport art from '@assets/artwork-placeholder.png';\nexport const v = [View, Play, art];\n",
      0,
    ],
    [
      'no-restricted-imports',
      'src/features/player/adapters/x.ts',
      "import { AppState } from 'react-native';\nexport const a = AppState;\n",
      1,
    ],
    [
      'no-restricted-imports',
      'src/features/player/adapters/x.ts',
      "import * as fs from 'expo-file-system';\nexport const f = fs;\n",
      0,
    ],
    [
      'no-restricted-syntax',
      'src/features/player/shell/X.tsx',
      'export function X() {\n  return <></>;\n}\n',
      1,
    ],
    [
      'no-restricted-syntax',
      'src/features/player/core/x.ts',
      'export function f(s: AbortSignal) {\n  s.throwIfAborted();\n}\n',
      1,
    ],
    [
      'no-restricted-properties',
      'src/features/player/__tests__/x.test.ts',
      "jest.mock('expo-file-system');\n",
      1,
    ],
    [
      'no-restricted-properties',
      'src/features/player/__tests__/x.test.ts',
      "export const s = jest.spyOn(Math, 'random');\n",
      1,
    ],
    [
      'no-restricted-properties',
      'src/features/player/__tests__/x.test.ts',
      'export const f = jest.fn();\n',
      0,
    ],
    ['no-restricted-properties', 'jest/x.ts', "jest.doMock('expo-crypto');\n", 1],
    [
      'no-restricted-properties',
      'src/shared/device/core/x.ts',
      'export const u = process.env.EXPO_PUBLIC_API_URL;\n',
      1,
    ],
    [
      'no-restricted-properties',
      'src/mobile/x.ts',
      'export const u = process.env.EXPO_PUBLIC_API_URL;\n',
      0,
    ],
    [
      'local/test-imports-through-doors',
      'src/features/player/__tests__/x.test.ts',
      "import { q } from '@features/player/core/queue';\nexport const a = q;\n",
      1,
    ],
    [
      'local/test-imports-through-doors',
      'src/features/player/__tests__/x.test.ts',
      "import { createPlayer } from '@features/player';\nexport const a = createPlayer;\n",
      0,
    ],
  ];

  for (const [ruleId, file, code, count] of cases) {
    it(`${ruleId} reports ${count} in ${file} for ${JSON.stringify(code.split('\n')[0])}`, async () => {
      const found = await errors(ruleId, file, code);
      assert.strictEqual(found.length, count, JSON.stringify(found));
      for (const { message } of found) {
        if (ruleId !== 'no-restricted-syntax' || !message.startsWith('Hermes')) {
          assert.match(message, /client\/src\/README\.md/);
        }
      }
    });
  }
});

describe('mobile eslint config has no warnings', () => {
  for (const file of [
    'src/mobile/index.ts',
    'src/features/player/view/useMediaSession.ts',
    'src/shared/apiClient/index.ts',
    'src/shared/ui/view/primitives/Banner.tsx',
    'src/shared/apiClient/__tests__/tracks.test.ts',
    'scripts/lint-changed-lines.mjs',
  ]) {
    it(`sets no rule to warn for ${file}`, async () => {
      const { rules } = await eslint.calculateConfigForFile(file);
      const warned = Object.entries(rules)
        .filter(([, setting]) => [setting].flat()[0] === 1 || [setting].flat()[0] === 'warn')
        .map(([name]) => name);
      assert.deepStrictEqual(warned, []);
    });
  }
});
