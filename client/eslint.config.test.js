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

async function restrictedImportMessages(filePath, code) {
  const [result] = await eslint.lintText(code, { filePath });
  return result.messages.filter((m) => m.ruleId === 'no-restricted-imports').map((m) => m.message);
}

describe('mobile eslint keeps view out of core', () => {
  const corePath = 'src/shared/failure/core/errors.ts';

  for (const source of ['@shared/auth/view/sessionExpired', '@features/library/view/Row']) {
    it(`reports an import of ${source} from core/`, async () => {
      const messages = await restrictedImportMessages(
        corePath,
        `import { x } from '${source}';\nexport const y = x;\n`,
      );
      assert.strictEqual(messages.length, 1);
      assert.match(messages[0], /core\/ never imports view\//);
      assert.match(messages[0], /client\/src\/README\.md/);
    });
  }

  for (const source of ['./view/Row', '../view', '../../features/library/view/Row']) {
    it(`reports the relative import ${source} from core/`, async () => {
      const messages = await restrictedImportMessages(
        corePath,
        `import { x } from '${source}';\nexport const y = x;\n`,
      );
      assert.strictEqual(
        messages.filter((message) => /never imports view/.test(message)).length,
        1,
      );
    });
  }

  for (const source of [
    '@shared/auth/viewModel',
    '@shared/preview/core/x',
    './viewHelpers',
    '../core/review',
  ]) {
    it(`does not report ${source}, which only looks like view/`, async () => {
      const messages = await restrictedImportMessages(
        corePath,
        `import { x } from '${source}';\nexport const y = x;\n`,
      );
      assert.ok(!messages.some((message) => /never imports view/.test(message)));
    });
  }

  it('allows view/ to import core/', async () => {
    const messages = await restrictedImportMessages(
      'src/shared/failure/view/Banner.ts',
      "import { x } from '@shared/failure/core/errors';\nexport const y = x;\n",
    );
    assert.strictEqual(messages.length, 0);
  });

  it('still reports a UI package imported from core/', async () => {
    const messages = await restrictedImportMessages(
      corePath,
      "import { useState } from 'react';\nexport const y = useState;\n",
    );
    assert.strictEqual(messages.length, 1);
  });

  it('allows a core/ file to import another core/ file', async () => {
    const messages = await restrictedImportMessages(
      corePath,
      "import { x } from '@shared/failure/core/other';\nexport const y = x;\n",
    );
    assert.strictEqual(messages.length, 0);
  });
});

describe('mobile eslint without type info (commit stage)', () => {
  const path = 'src/shared/failure/core/errors.ts';
  const code = 'export const data = 1;\nPromise.resolve(data);\n';

  it('drops type-aware rules but keeps untyped ones', () => {
    const rules = diffScopedRuleIds(path, code, { ESLINT_WITHOUT_TYPES: '1' });
    assert.ok(!rules.includes('@typescript-eslint/no-floating-promises'));
    assert.ok(rules.includes('id-match'));
    const untypedTsRules = diffScopedRuleIds(
      path,
      'export const data = 1;\nnamespace Box {}\nexport const bang = [data][0]!;\n',
      { ESLINT_WITHOUT_TYPES: '1' },
    );
    assert.ok(untypedTsRules.includes('@typescript-eslint/no-namespace'));
    assert.ok(untypedTsRules.includes('@typescript-eslint/no-non-null-assertion'));
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
      (await rawStyleErrors('src/shared/ui/view/primitives/TextField.tsx', code)).length,
      1,
    );
  });
  it('allows 0, 1 and flex', async () => {
    const code =
      "import { StyleSheet } from 'react-native';\nexport const styles = StyleSheet.create({ a: { flex: 3, margin: 0, opacity: 1 } });\n";
    assert.strictEqual(
      (await rawStyleErrors('src/shared/ui/view/primitives/TextField.tsx', code)).length,
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
      'no-restricted-syntax',
      'src/features/player/core/x.ts',
      'export const f = (a: Promise<void>, b: Promise<void>) => Promise.race([a, b]);\n',
      1,
    ],
    [
      'no-restricted-syntax',
      'src/shared/offline/shell/x.ts',
      'export const f = (a: Promise<void>, b: Promise<void>) => Promise.race([a, b]);\n',
      1,
    ],
    [
      'no-restricted-syntax',
      'src/features/player/view/x.ts',
      'export const f = (a: Promise<void>, b: Promise<void>) => Promise.race([a, b]);\n',
      1,
    ],
    [
      'no-restricted-syntax',
      'src/features/player/core/x.ts',
      'export const f = (a: Promise<void>, b: Promise<void>) => Promise.all([a, b]);\n',
      0,
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
      'no-restricted-properties',
      'src/features/library/view/x.ts',
      "export const f = (c: { invalidateQueries: (o: object) => void }) => c.invalidateQueries({ queryKey: ['a'] });\n",
      1,
    ],
    [
      'no-restricted-properties',
      'src/mobile/x.ts',
      "export const f = (c: { invalidateQueries: (o: object) => void }) => c.invalidateQueries({ queryKey: ['a'] });\n",
      1,
    ],
    [
      'no-restricted-properties',
      'src/shared/events/shell/x.ts',
      "export const f = (c: { invalidateQueries: (o: object) => void }) => c.invalidateQueries({ queryKey: ['a'] });\n",
      0,
    ],
    [
      'no-restricted-properties',
      'src/shared/query/shell/x.ts',
      "export const f = (c: { invalidateQueries: (o: object) => void }) => c.invalidateQueries({ queryKey: ['a'] });\n",
      0,
    ],
    [
      'no-restricted-properties',
      'src/shared/events/__tests__/x.test.ts',
      "export const f = (c: { invalidateQueries: (o: object) => void }) => c.invalidateQueries({ queryKey: ['a'] });\n",
      0,
    ],
    [
      'no-restricted-properties',
      'src/shared/events/shell/x.ts',
      'export const u = process.env.EXPO_PUBLIC_API_URL;\n',
      1,
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

async function statusLiteralErrors(code) {
  const [result] = await eslint.lintText(code, { filePath: 'src/shared/query/core/retryDelay.ts' });
  return result.messages.filter(
    (m) => m.ruleId === 'no-restricted-syntax' && m.message.includes('HTTP status'),
  );
}

describe('mobile eslint config bans a bare HTTP status number', () => {
  for (const [name, code] of [
    ['status on the left', 'export const a = (error: { status: number }) => error.status >= 500;'],
    ['status on the right', 'export const a = (status: number) => 500 <= status;'],
    [
      'a case label',
      'export const a = (status: number) => { switch (status) { case 404: return 1; default: return 0; } };',
    ],
    ['a set of statuses', 'export const RETRYABLE_STATUSES = new Set([401, 408, 429]);'],
  ]) {
    it(`reports ${name}`, async () => {
      assert.ok((await statusLiteralErrors(code)).length >= 1);
    });
  }

  it('allows a named constant and a number that is not a status', async () => {
    const errors = await statusLiteralErrors(
      'const HTTP_X = 500;\nexport const a = (status: number, count: number) => status >= HTTP_X || count > 5000;',
    );
    assert.strictEqual(errors.length, 0);
  });
});

async function staleLiteralErrors(code) {
  const [result] = await eslint.lintText(code, { filePath: 'src/shared/query/core/retryDelay.ts' });
  return result.messages.filter(
    (m) => m.ruleId === 'no-restricted-syntax' && m.message.includes('goes stale'),
  );
}

describe('mobile eslint config requires a named staleTime policy', () => {
  for (const [name, code] of [
    ['a number', 'export const a = { staleTime: 60000 };'],
    ['Infinity', 'export const a = { staleTime: Infinity };'],
    ['zero', 'export const a = { staleTime: 0 };'],
    ['a product', 'export const a = { staleTime: 5 * 60_000 };'],
    ['a negative number', 'export const a = { staleTime: -1 };'],
    ['Number.POSITIVE_INFINITY', 'export const a = { staleTime: Number.POSITIVE_INFINITY };'],
    ['a quoted key', "export const a = { ['staleTime']: 1 };"],
  ]) {
    it(`reports ${name}`, async () => {
      assert.ok((await staleLiteralErrors(code)).length >= 1);
    });
  }

  it('allows a named policy', async () => {
    const errors = await staleLiteralErrors(
      'const STALE = { minute: 1 };\nexport const a = { staleTime: STALE.minute };',
    );
    assert.strictEqual(errors.length, 0);
  });
});

async function clockWaitErrors(code, filePath) {
  const [result] = await eslint.lintText(code, { filePath });
  return result.messages.filter(
    (m) => m.ruleId === 'no-restricted-syntax' && m.message.includes('real clock'),
  );
}

describe('mobile eslint config requires a locale when case folding', () => {
  const PATH = 'src/shared/acquisition/state/trackStatusStore.ts';
  const foldErrors = async (code) => {
    const [result] = await eslint.lintText(code, { filePath: PATH });
    return result.messages.filter(
      (m) => m.ruleId === 'no-restricted-syntax' && m.message.includes('Case folding'),
    );
  };

  it('reports toLocaleLowerCase and toLocaleUpperCase without a locale', async () => {
    assert.strictEqual(
      (await foldErrors('export const a = (s: string) => s.toLocaleLowerCase();')).length,
      1,
    );
    assert.strictEqual(
      (await foldErrors('export const a = (s: string) => s.toLocaleUpperCase();')).length,
      1,
    );
  });

  it('allows a named locale and the plain folds', async () => {
    assert.strictEqual(
      (await foldErrors("export const a = (s: string) => s.toLocaleLowerCase('en-US');")).length,
      0,
    );
    assert.strictEqual(
      (await foldErrors('export const a = (s: string) => s.toLowerCase();')).length,
      0,
    );
  });
});

describe('mobile eslint config bans waiting on the real clock in a test', () => {
  const TEST_PATH = 'src/shared/query/__tests__/retryDelay.test.ts';

  for (const [name, code] of [
    ['a sleep promise', 'export const a = () => new Promise((r) => setTimeout(r, 50));'],
    ['a bare timer', 'export const a = () => { setTimeout(() => undefined, 0); };'],
    ['setInterval', 'export const a = () => { setInterval(() => undefined, 5); };'],
    ['setImmediate', 'export const a = () => new Promise((r) => setImmediate(r));'],
    ['a timer on global', 'export const a = () => { global.setTimeout(() => undefined, 5); };'],
    ['a timer on window', 'export const a = () => { window.setInterval(() => undefined, 5); };'],
    [
      'node:timers/promises',
      "import { setTimeout as sleep } from 'node:timers/promises';\nexport const a = sleep;",
    ],
    ['timers', "import { setImmediate as soon } from 'timers';\nexport const a = soon;"],
    ['require of timers/promises', "export const a = require('timers/promises');"],
    [
      'a timer on globalThis',
      'export const a = () => { globalThis.setTimeout(() => undefined, 5); };',
    ],
    ['a timer on self', 'export const a = () => { self.setTimeout(() => undefined, 5); };'],
    ['a dynamic import of timers', "export const a = () => import('node:timers/promises');"],
    ['a re-export of timers', "export { setTimeout } from 'node:timers/promises';"],
    ['a star re-export of timers', "export * from 'timers';"],
  ]) {
    it(`reports ${name}`, async () => {
      assert.ok((await clockWaitErrors(code, TEST_PATH)).length >= 1);
    });
  }

  it('reports a wait in a .tsx test and in the root __tests__ folder', async () => {
    const code = 'export const a = () => { setTimeout(() => undefined, 0); };';
    assert.ok((await clockWaitErrors(code, 'src/shared/ui/__tests__/Thing.test.tsx')).length >= 1);
    assert.ok((await clockWaitErrors(code, '__tests__/rootLayout.test.ts')).length >= 1);
  });

  it('still applies the rules banned everywhere in a test file', async () => {
    const [result] = await eslint.lintText('export const a = { staleTime: 60000 };', {
      filePath: TEST_PATH,
    });
    assert.ok(result.messages.some((m) => m.ruleId === 'no-restricted-syntax'));
  });

  it('reports a wait in jest/__tests__ and leaves the Hermes abort check to src', async () => {
    const wait = 'export const a = () => { setTimeout(() => undefined, 0); };';
    assert.ok((await clockWaitErrors(wait, 'jest/__tests__/queryHarness.test.tsx')).length >= 1);
    const [result] = await eslint.lintText(
      'export const a = new AbortController().signal.throwIfAborted;',
      {
        filePath: 'jest/__tests__/setup-env.test.ts',
      },
    );
    assert.strictEqual(
      result.messages.filter((m) => m.ruleId === 'no-restricted-syntax').length,
      0,
    );
  });

  it('allows setTimeout in production code and jest.setTimeout in a test', async () => {
    assert.strictEqual(
      (
        await clockWaitErrors(
          'export const a = () => { setTimeout(() => undefined, 0); };',
          'src/shared/query/core/retryDelay.ts',
        )
      ).length,
      0,
    );
    assert.strictEqual((await clockWaitErrors('jest.setTimeout(20000);', TEST_PATH)).length, 0);
  });
});

describe('mobile eslint config asks the acquisition predicate, not the raw status', () => {
  const PATH = 'src/features/library/view/useTrackMenu.ts';
  const rawStatusErrors = async (code, filePath = PATH) => {
    const [result] = await eslint.lintText(code, { filePath });
    return result.messages.filter(
      (m) => m.ruleId === 'no-restricted-syntax' && m.message.includes('acquisition predicate'),
    );
  };

  for (const [name, code] of [
    [
      'snake case ===',
      "export const a = (t: { acquisition_status: string }) => t.acquisition_status === 'ready';",
    ],
    [
      'camel case !==',
      "export const a = (t: { acquisitionStatus: string }) => t.acquisitionStatus !== 'failed';",
    ],
    [
      'an optional chain',
      "export const a = (t?: { acquisitionStatus: string }) => t?.acquisitionStatus === 'pending';",
    ],
  ]) {
    it(`reports ${name}`, async () => {
      assert.strictEqual((await rawStatusErrors(code)).length, 1);
    });
  }

  it('allows an unrelated status and a switch', async () => {
    assert.strictEqual(
      (await rawStatusErrors("export const a = (s: { status: string }) => s.status === 'ready';"))
        .length,
      0,
    );
    assert.strictEqual(
      (
        await rawStatusErrors(
          "export const a = (t: { acquisition_status: string }) => { switch (t.acquisition_status) { case 'ready': return 1; default: return 0; } };",
        )
      ).length,
      0,
    );
  });
});
