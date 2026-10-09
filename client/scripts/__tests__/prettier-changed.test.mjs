import { describe, test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const script = join(dirname(fileURLToPath(import.meta.url)), '..', 'prettier-changed.mjs');

const CLEAN = 'export const a = 1;\n';
const DIRTY = 'export const a   =   1\n';

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
  const root = mkdtempSync(join(tmpdir(), 'prettier-changed-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  git(root, 'init', '-q');
  git(root, 'config', 'user.email', 'probe@example.com');
  git(root, 'config', 'user.name', 'probe');
  git(root, 'config', 'commit.gpgsign', 'false');
  write(root, { '.prettierrc': '{"singleQuote": true}\n', ...baseFiles });
  git(root, 'add', '-A');
  git(root, 'commit', '-qm', 'base');
  const base = git(root, 'rev-parse', 'HEAD');
  write(root, changedFiles);
  git(root, 'add', '-A');
  git(root, 'commit', '-qm', 'change', '--allow-empty');
  return { root, base };
}

function run(cwd, ...args) {
  return spawnSync(process.execPath, [script, ...args], { cwd, encoding: 'utf8' });
}

describe('prettier-changed', () => {
  test('exits 1 naming a changed file that is not Prettier-clean', (t) => {
    const { root, base } = repo(
      t,
      {},
      { 'src/a.ts': DIRTY, 'src/b.tsx': 'export const b = <div />;\n' },
    );
    const r = run(root, base);
    assert.equal(r.status, 1);
    assert.match(r.stderr, /^src\/a\.ts$/m);
    assert.doesNotMatch(r.stderr, /b\.tsx/);
  });

  test('exits 0 when every changed file is clean', (t) => {
    const { root, base } = repo(t, {}, { 'src/a.ts': CLEAN });
    assert.equal(run(root, base).status, 0);
  });

  test('ignores unformatted files the change did not touch', (t) => {
    const { root, base } = repo(t, { 'src/old.ts': DIRTY }, { 'src/a.ts': CLEAN });
    const r = run(root, base);
    assert.equal(r.status, 0);
    assert.doesNotMatch(r.stderr, /old\.ts/);
  });

  test('ignores unformatted files outside src and non-ts extensions', (t) => {
    const { root, base } = repo(t, {}, { 'scripts/x.ts': DIRTY, 'src/y.js': DIRTY });
    const r = run(root, base);
    assert.equal(r.status, 0);
    assert.match(r.stdout, /no changed files/);
  });

  test('checks a renamed file that is still unformatted', (t) => {
    const { root, base } = repo(t, { 'src/old.ts': DIRTY }, {});
    git(root, 'mv', 'src/old.ts', 'src/new.ts');
    git(root, 'commit', '-qam', 'rename');
    const r = run(root, base);
    assert.equal(r.status, 1);
    assert.match(r.stderr, /^src\/new\.ts$/m);
  });

  test('exits 2 on an unresolvable base', (t) => {
    const { root } = repo(t, {}, {});
    assert.equal(run(root, 'no-such-ref').status, 2);
  });
});
