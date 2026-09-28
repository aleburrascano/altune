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

describe('lint-changed-lines base and diff handling', { concurrency: true }, () => {
  test('passes strings, template literals, regexes and JSX text that contain slashes or URLs', async (t) => {
    const { root, base } = repo(
      t,
      {},
      {
        '__tests__/strings.test.tsx': [
          "export const url = 'https://x.dev';",
          'export const s = "a // b /* c */";',
          'export const t = `c // d`;',
          'export const r = /https?:\\/\\/x/;',
          'export const j = <a href="https://x.dev">see http://y.dev</a>;',
          '',
        ].join('\n'),
        'jest/url.js': "module.exports = 'https://x.dev';\n",
      },
    );
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
    const { root, base } = repo(
      t,
      { '__tests__/before.test.ts': '// kept\nexport const a = 1;\n' },
      {},
    );
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

  test('exits 2 when the base looks like a git option', async (t) => {
    const { root } = repo(t, {}, { '__tests__/a.test.ts': 'export const a = 1;\n' });
    for (const bad of ['--', '--stat', '--output=/tmp/lint-changed-lines-opt']) {
      const r = await lint(root, bad);
      assert.equal(r.status, 2, `${bad}: ${r.stdout}${r.stderr}`);
    }
  });

  test('exits 2 when the base ref cannot be resolved', async (t) => {
    const { root } = repo(t, {}, { '__tests__/a.test.ts': 'export const a = 1;\n' });
    for (const bad of ['no-such-ref', '0000000000000000000000000000000000000000']) {
      const r = await lint(root, bad);
      assert.equal(r.status, 2, `${bad}: ${r.stdout}${r.stderr}`);
    }
  });

  test('does not flag a comment moved into a new file', async (t) => {
    const { root, base } = repo(
      t,
      { '__tests__/before.test.ts': '// kept\nexport const a = 1;\n' },
      {},
    );
    rmSync(join(root, '__tests__/before.test.ts'));
    write(root, { '__tests__/moved-into/after.test.ts': '// kept\nexport const a = 1;\n' });
    git(root, 'add', '-A');
    git(root, 'commit', '-qm', 'move');
    const r = await lint(root, base);
    assert.equal(r.status, 0, r.stdout + r.stderr);
  });
});

describe('lint-changed-lines on hostile paths and bases', { concurrency: true }, () => {
  test('never lets an option-shaped base write a file', async (t) => {
    const { existsSync } = await import('node:fs');
    const { root } = repo(t, {}, { '__tests__/a.test.ts': 'export const a = 1;\n' });
    const out = join(root, 'written-by-git');
    const r = await lint(root, `--output=${out}`);
    assert.equal(r.status, 2, r.stdout + r.stderr);
    assert.equal(existsSync(out), false);
  });

  test('exits 2 when the base is a single-dash option', async (t) => {
    const { root } = repo(t, {}, { '__tests__/a.test.ts': 'export const a = 1;\n// note\n' });
    for (const bad of ['-', '-R', '-p', '-Snote']) {
      const r = await lint(root, bad);
      assert.equal(r.status, 2, `${bad}: ${r.stdout}${r.stderr}`);
    }
  });

  test('exits 2 when the base is a range, a tree or a blob rather than one commit', async (t) => {
    const { root, base } = repo(t, {}, { '__tests__/a.test.ts': 'export const a = 1;\n// note\n' });
    for (const bad of [
      `${base}..HEAD`,
      'HEAD...HEAD',
      'HEAD..HEAD',
      'HEAD^{tree}',
      'HEAD:__tests__/a.test.ts',
    ]) {
      const r = await lint(root, bad);
      assert.equal(r.status, 2, `${bad}: ${r.stdout}${r.stderr}`);
    }
  });

  test('does not flag the comments of an unchanged file copied as is', async (t) => {
    const body =
      '// kept\nexport const a = 1;\nexport const b = 2;\nexport const c = 3;\nexport const d = 4;\n';
    const { root, base } = repo(
      t,
      { '__tests__/source.test.ts': body },
      { '__tests__/copy.test.ts': body },
    );
    const r = await lint(root, base);
    assert.equal(r.status, 0, r.stdout + r.stderr);
  });

  test('passes a change that only deletes a file, even one with a space in its path', async (t) => {
    const { root, base } = repo(
      t,
      { '__tests__/go ne.test.ts': '// old\nexport const a = 1;\n' },
      {},
    );
    git(root, 'rm', '-q', '__tests__/go ne.test.ts');
    git(root, 'commit', '-qm', 'delete');
    const r = await lint(root, base);
    assert.equal(r.status, 0, r.stdout + r.stderr);
  });

  test('passes an empty diff', async (t) => {
    const { root } = repo(t, { '__tests__/a.test.ts': '// old\nexport const a = 1;\n' }, {});
    const r = await lint(root, 'HEAD');
    assert.equal(r.status, 0, r.stdout + r.stderr);
  });
});

describe('lint-changed-lines leaves comments to the whole-file lint rule', () => {
  test('does not report a comment added to a changed file', async (t) => {
    const { root, base } = repo(t, {}, { 'jest/added.js': 'module.exports = 1;\n// note\n' });
    const r = await lint(root, base);
    assert.equal(r.status, 0, r.stdout + r.stderr);
    assert.doesNotMatch(r.stdout, /comment/);
  });
});
