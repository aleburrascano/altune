import { describe, test } from 'node:test';
import assert from 'node:assert/strict';
import { spawn, spawnSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const script = join(dirname(fileURLToPath(import.meta.url)), '..', 'lint-changed-lines.mjs');

function git(cwd, ...args) {
  const r = spawnSync('git', args, { cwd, encoding: 'utf8' });
  assert.equal(r.status, 0, r.stderr);
  return r.stdout.trim();
}

function write(root, files) {
  for (const [path, body] of Object.entries(files)) {
    mkdirSync(dirname(join(root, path)), { recursive: true });
    writeFileSync(join(root, path), body);
  }
}

function repo(t, baseFiles, changedFiles) {
  const root = mkdtempSync(join(tmpdir(), 'lint-changed-lines-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  git(root, 'init', '-q');
  git(root, 'config', 'user.email', 'probe@example.com');
  git(root, 'config', 'user.name', 'probe');
  git(root, 'config', 'commit.gpgsign', 'false');
  write(root, { 'jest/keep.js': 'module.exports = 0;\n', ...baseFiles });
  git(root, 'add', '-A');
  git(root, 'commit', '-qm', 'base');
  const base = git(root, 'rev-parse', 'HEAD');
  write(root, changedFiles);
  git(root, 'add', '-A');
  git(root, 'commit', '-qm', 'change', '--allow-empty');
  return { root, base };
}

function lint(cwd, ...args) {
  return new Promise((resolve) => {
    const child = spawn(process.execPath, [script, ...args], { cwd });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (d) => (stdout += d));
    child.stderr.on('data', (d) => (stderr += d));
    child.on('close', (status) => resolve({ status, stdout, stderr }));
  });
}

describe('lint-changed-lines comment gate', { concurrency: true }, () => {
  test('flags a comment added to test files, jest setup and root configs, naming file and line', async (t) => {
    const { root, base } = repo(
      t,
      {
        '__tests__/rootLayout.integration.test.tsx': 'export const a = <div />;\n',
        'jest/setup-env.js': 'module.exports = {};\n',
      },
      {
        '__tests__/rootLayout.integration.test.tsx': 'export const a = <div />;\n// note\n',
        'jest/setup-env.js': 'module.exports = {};\n// note\n',
        'src/features/x/__tests__/x.test.tsx': 'export const b = 1;\n\n// note\n',
        'babel.config.js': '/* note */\nmodule.exports = {};\n',
        'scripts/tool.mjs': 'export default 1; // note\n',
        'eslint-rules/rule.cjs': 'module.exports = 1;\n// note\n',
      },
    );
    const r = await lint(root, base);
    assert.equal(r.status, 1, r.stdout + r.stderr);
    for (const hit of [
      '__tests__/rootLayout.integration.test.tsx:2 ',
      'jest/setup-env.js:2 ',
      'src/features/x/__tests__/x.test.tsx:3 ',
      'babel.config.js:1 ',
      'scripts/tool.mjs:1 ',
      'eslint-rules/rule.cjs:2 ',
    ]) {
      assert.ok(r.stdout.includes(hit), `expected a hit for ${hit} in:\n${r.stdout}`);
    }
  });

  test('flags a JSX comment and a new suppression in a test file', async (t) => {
    const { root, base } = repo(t, {}, {
      '__tests__/view.test.tsx':
        'export const j = <p>{/* inner */}</p>;\n// @ts-expect-error\nexport const n: number = 1;\n',
    });
    const r = await lint(root, base);
    assert.equal(r.status, 1, r.stdout + r.stderr);
    assert.ok(r.stdout.includes('__tests__/view.test.tsx:1 '), r.stdout);
    assert.match(r.stdout, /__tests__\/view\.test\.tsx:2\s+new suppression/);
  });

  test('passes strings, template literals, regexes and JSX text that contain slashes or URLs', async (t) => {
    const { root, base } = repo(t, {}, {
      '__tests__/strings.test.tsx': [
        "export const url = 'https://x.dev';",
        'export const s = "a // b /* c */";',
        'export const t = `c // d`;',
        'export const r = /https?:\\/\\/x/;',
        'export const j = <a href="https://x.dev">see http://y.dev</a>;',
        '',
      ].join('\n'),
      'jest/url.js': "module.exports = 'https://x.dev';\n",
    });
    const r = await lint(root, base);
    assert.equal(r.status, 0, r.stdout + r.stderr);
  });

  test('passes a change that only deletes comment lines', async (t) => {
    const { root, base } = repo(
      t,
      { '__tests__/old.test.ts': '// old\nexport const a = 1;\n// older\nexport const b = 2;\n' },
      { '__tests__/old.test.ts': 'export const a = 1;\nexport const b = 2;\n' },
    );
    const r = await lint(root, base);
    assert.equal(r.status, 0, r.stdout + r.stderr);
  });

  test('passes a pure rename of a file whose comments already existed on base', async (t) => {
    const { root, base } = repo(t, { '__tests__/before.test.ts': '// kept\nexport const a = 1;\n' }, {});
    git(root, 'mv', '__tests__/before.test.ts', '__tests__/after.test.ts');
    git(root, 'commit', '-qm', 'rename');
    const r = await lint(root, base);
    assert.equal(r.status, 0, r.stdout + r.stderr);
  });

  test('fails closed when the base ref does not resolve', async (t) => {
    const { root } = repo(t, {}, { '__tests__/a.test.ts': 'export const a = 1;\n' });
    for (const bad of ['no-such-ref', '0000000000000000000000000000000000000000']) {
      const r = await lint(root, bad);
      assert.notEqual(r.status, 0, `${bad}: ${r.stdout}`);
    }
  });

  test('exits 2 when no base ref is given', async (t) => {
    const { root } = repo(t, {}, {});
    const r = await lint(root);
    assert.equal(r.status, 2, r.stdout + r.stderr);
  });
});
