import { describe, test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const script = join(dirname(fileURLToPath(import.meta.url)), '..', 'react-doctor-changed-lines.mjs');

const OLD_CHAIN = 'export const evens = xs.filter((x) => x % 2).map((x) => x * 2);';
const NEW_CHAIN = 'export const odds = ys.filter((y) => y % 3).map((y) => y * 3);';
const PLAIN = 'export const one = 1;';

function git(cwd, ...args) {
  const r = spawnSync('git', args, { cwd, encoding: 'utf8' });
  assert.equal(r.status, 0, r.stderr);
  return r.stdout.trim();
}

function write(root, files) {
  for (const [path, body] of Object.entries(files)) {
    if (body === null) {
      rmSync(join(root, path));
      continue;
    }
    mkdirSync(dirname(join(root, path)), { recursive: true });
    writeFileSync(join(root, path), body);
  }
}

function lines(...rows) {
  return `${rows.join('\n')}\n`;
}

function repo(t, baseFiles, changedFiles) {
  const root = mkdtempSync(join(tmpdir(), 'react-doctor-changed-lines-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  git(root, 'init', '-q');
  git(root, 'config', 'user.email', 'probe@example.com');
  git(root, 'config', 'user.name', 'probe');
  git(root, 'config', 'commit.gpgsign', 'false');
  write(root, { 'client/keep.ts': lines(PLAIN), ...baseFiles });
  git(root, 'add', '-A');
  git(root, 'commit', '-qm', 'base');
  const base = git(root, 'rev-parse', 'HEAD');
  write(root, changedFiles);
  git(root, 'add', '-A');
  git(root, 'commit', '-qm', 'change', '--allow-empty');
  return { root, base };
}

function warning(filePath, line) {
  return {
    filePath,
    plugin: 'react-doctor',
    rule: 'js-combine-iterations',
    severity: 'warning',
    message: 'loops twice',
    line,
    column: 1,
  };
}

function check(root, base, report) {
  const reportPath = join(root, 'doctor-report.json');
  writeFileSync(reportPath, JSON.stringify(report));
  const r = spawnSync(process.execPath, [script, base, reportPath], {
    cwd: join(root, 'client'),
    encoding: 'utf8',
  });
  return { status: r.status, out: `${r.stdout}${r.stderr}` };
}

describe('react-doctor-changed-lines holds changed lines to zero', { concurrency: true }, () => {
  test('fails a warning on a line the change added', (t) => {
    const { root, base } = repo(t, { 'client/src/a.ts': lines(PLAIN) }, { 'client/src/a.ts': lines(PLAIN, NEW_CHAIN) });
    const r = check(root, base, { diagnostics: [warning('src/a.ts', 2)] });
    assert.equal(r.status, 1, r.out);
    assert.match(r.out, /src\/a\.ts:2:1 warning react-doctor\/js-combine-iterations/);
  });

  test('fails a warning on an edited line', (t) => {
    const { root, base } = repo(t, { 'client/src/a.ts': lines(OLD_CHAIN) }, { 'client/src/a.ts': lines(NEW_CHAIN) });
    const r = check(root, base, { diagnostics: [warning('src/a.ts', 1)] });
    assert.equal(r.status, 1, r.out);
  });

  test('fails a warning in a new file', (t) => {
    const { root, base } = repo(t, {}, { 'client/src/new.tsx': lines(NEW_CHAIN) });
    const r = check(root, base, { diagnostics: [warning('src/new.tsx', 1)] });
    assert.equal(r.status, 1, r.out);
  });

  test('counts uncommitted edits, as the commit hook sees them', (t) => {
    const { root, base } = repo(t, { 'client/src/a.ts': lines(PLAIN) }, {});
    write(root, { 'client/src/a.ts': lines(PLAIN, NEW_CHAIN) });
    const r = check(root, base, { diagnostics: [warning('src/a.ts', 2)] });
    assert.equal(r.status, 1, r.out);
  });
});

describe('react-doctor-changed-lines leaves old warnings to the ceiling', { concurrency: true }, () => {
  test('passes a warning on a line the change did not touch, in a changed file', (t) => {
    const { root, base } = repo(
      t,
      { 'client/src/a.ts': lines(OLD_CHAIN, PLAIN) },
      { 'client/src/a.ts': lines(OLD_CHAIN, PLAIN, 'export const two = 2;') },
    );
    const r = check(root, base, { diagnostics: [warning('src/a.ts', 1)] });
    assert.equal(r.status, 0, r.out);
  });

  test('passes a warning in a file the change did not touch', (t) => {
    const { root, base } = repo(
      t,
      { 'client/src/old.ts': lines(OLD_CHAIN), 'client/src/a.ts': lines(PLAIN) },
      { 'client/src/a.ts': lines(PLAIN, 'export const two = 2;') },
    );
    const r = check(root, base, { diagnostics: [warning('src/old.ts', 1)] });
    assert.equal(r.status, 0, r.out);
  });

  test('passes an old warning moved into another file', (t) => {
    const { root, base } = repo(
      t,
      { 'client/src/from.ts': lines(PLAIN, OLD_CHAIN) },
      { 'client/src/from.ts': lines(PLAIN), 'client/src/to.ts': lines('export const two = 2;', OLD_CHAIN) },
    );
    const r = check(root, base, { diagnostics: [warning('src/to.ts', 2)] });
    assert.equal(r.status, 0, r.out);
  });

  test('passes an old warning in a renamed file', (t) => {
    const { root, base } = repo(t, { 'client/src/before.ts': lines(OLD_CHAIN) }, {});
    git(root, 'mv', 'client/src/before.ts', 'client/src/after.ts');
    git(root, 'commit', '-qm', 'rename');
    const r = check(root, base, { diagnostics: [warning('src/after.ts', 1)] });
    assert.equal(r.status, 0, r.out);
  });

  test('fails a new warning added beside moved code', (t) => {
    const { root, base } = repo(
      t,
      { 'client/src/from.ts': lines(PLAIN, OLD_CHAIN) },
      { 'client/src/from.ts': lines(PLAIN), 'client/src/to.ts': lines(OLD_CHAIN, NEW_CHAIN) },
    );
    const r = check(root, base, { diagnostics: [warning('src/to.ts', 1), warning('src/to.ts', 2)] });
    assert.equal(r.status, 1, r.out);
    assert.match(r.out, /src\/to\.ts:2:/);
    assert.doesNotMatch(r.out, /src\/to\.ts:1:/);
  });

  test('ignores changes outside the directory it runs in', (t) => {
    const { root, base } = repo(t, {}, { 'other/src/a.ts': lines(NEW_CHAIN) });
    const r = check(root, base, { diagnostics: [warning('src/a.ts', 1)] });
    assert.equal(r.status, 0, r.out);
  });
});

describe('react-doctor-changed-lines fails closed', { concurrency: true }, () => {
  test('fails when the report carries an error and no diagnostics', (t) => {
    const { root, base } = repo(t, {}, { 'client/src/a.ts': lines(PLAIN) });
    const r = check(root, base, { ok: false, error: { message: 'scan crashed' } });
    assert.notEqual(r.status, 0, r.out);
    assert.match(r.out, /scan crashed/);
  });

  test('fails when the report is not JSON', (t) => {
    const { root, base } = repo(t, {}, { 'client/src/a.ts': lines(PLAIN) });
    const reportPath = join(root, 'doctor-report.json');
    writeFileSync(reportPath, 'not json');
    const r = spawnSync(process.execPath, [script, base, reportPath], { cwd: join(root, 'client'), encoding: 'utf8' });
    assert.notEqual(r.status, 0, r.stdout);
  });

  test('exits 2 when no base ref is given', (t) => {
    const { root } = repo(t, {}, {});
    const r = spawnSync(process.execPath, [script], { cwd: join(root, 'client'), encoding: 'utf8' });
    assert.equal(r.status, 2, r.stdout + r.stderr);
  });

  test('exits 2 when the base looks like a git option', (t) => {
    const { root } = repo(t, {}, {});
    const r = check(root, '--output=/tmp/x', { diagnostics: [] });
    assert.equal(r.status, 2, r.out);
  });

  test('exits 2 when the base ref cannot be resolved', (t) => {
    const { root } = repo(t, {}, {});
    const r = check(root, 'no-such-ref', { diagnostics: [] });
    assert.equal(r.status, 2, r.out);
  });
});
