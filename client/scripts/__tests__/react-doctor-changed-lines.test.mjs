import { describe, test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const script = join(
  dirname(fileURLToPath(import.meta.url)),
  '..',
  'react-doctor-changed-lines.mjs',
);

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

function spanning(filePath, body, fromLine, toLine) {
  const rows = body.split('\n');
  const offset = rows.slice(0, fromLine - 1).join('\n').length + (fromLine > 1 ? 1 : 0);
  const end = rows.slice(0, toLine).join('\n').length;
  return {
    ...warning(filePath, fromLine),
    rule: 'jsx-no-jsx-as-prop',
    offset,
    length: end - offset,
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

function listNew(root, base, head, baseReport, env = {}) {
  const headPath = join(root, 'doctor-report.json');
  writeFileSync(headPath, JSON.stringify(head));
  const args = [script, '--list-new', base, headPath];
  if (baseReport !== undefined) {
    const basePath = join(root, 'base-report.json');
    if (baseReport !== null) writeFileSync(basePath, JSON.stringify(baseReport));
    args.push(basePath);
  }
  const r = spawnSync(process.execPath, args, {
    cwd: join(root, 'client'),
    encoding: 'utf8',
    env: { ...process.env, MAX_WARNINGS: '', ...env },
  });
  return { status: r.status, out: `${r.stdout}${r.stderr}` };
}

function unused(filePath, line, name) {
  return {
    filePath,
    plugin: 'deslop',
    rule: 'unused-export',
    severity: 'warning',
    message: `Unused export: \`${name}\` is exported but no module imports it`,
    line,
    column: 1,
  };
}

const JSX_BEFORE = lines(
  'export const A = () => (',
  '  <Row',
  '    left={<Icon />}',
  '    right={null}',
  '  />',
  ');',
);
const JSX_AFTER = lines(
  'export const A = () => (',
  '  <Row',
  '    left={<Icon />}',
  '    right={<Badge />}',
  '  />',
  ');',
);

describe('react-doctor-changed-lines holds changed lines to zero', { concurrency: true }, () => {
  test('fails a warning on a line the change added', (t) => {
    const { root, base } = repo(
      t,
      { 'client/src/a.ts': lines(PLAIN) },
      { 'client/src/a.ts': lines(PLAIN, NEW_CHAIN) },
    );
    const r = check(root, base, { diagnostics: [warning('src/a.ts', 2)] });
    assert.equal(r.status, 1, r.out);
    assert.match(r.out, /src\/a\.ts:2:1 warning react-doctor\/js-combine-iterations/);
  });

  test('fails a warning on an edited line', (t) => {
    const { root, base } = repo(
      t,
      { 'client/src/a.ts': lines(OLD_CHAIN) },
      { 'client/src/a.ts': lines(NEW_CHAIN) },
    );
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

describe(
  'react-doctor-changed-lines leaves old warnings to the ceiling',
  { concurrency: true },
  () => {
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
        {
          'client/src/from.ts': lines(PLAIN),
          'client/src/to.ts': lines('export const two = 2;', OLD_CHAIN),
        },
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
      const r = check(root, base, {
        diagnostics: [warning('src/to.ts', 1), warning('src/to.ts', 2)],
      });
      assert.equal(r.status, 1, r.out);
      assert.match(r.out, /src\/to\.ts:2:/);
      assert.doesNotMatch(r.out, /src\/to\.ts:1:/);
    });

    test('ignores changes outside the directory it runs in', (t) => {
      const { root, base } = repo(t, {}, { 'other/src/a.ts': lines(NEW_CHAIN) });
      const r = check(root, base, { diagnostics: [warning('src/a.ts', 1)] });
      assert.equal(r.status, 0, r.out);
    });
  },
);

describe('react-doctor-changed-lines reads the whole reported range', { concurrency: true }, () => {
  test('fails a multi-line warning reported above the edited line', (t) => {
    const { root, base } = repo(
      t,
      { 'client/src/a.tsx': JSX_BEFORE },
      { 'client/src/a.tsx': JSX_AFTER },
    );
    const r = check(root, base, { diagnostics: [spanning('src/a.tsx', JSX_AFTER, 2, 5)] });
    assert.equal(r.status, 1, r.out);
    assert.match(r.out, /src\/a\.tsx:2:1 warning react-doctor\/jsx-no-jsx-as-prop/);
  });

  test('passes a multi-line warning whose range ends above the edited line', (t) => {
    const { root, base } = repo(
      t,
      { 'client/src/a.tsx': JSX_BEFORE },
      { 'client/src/a.tsx': JSX_AFTER },
    );
    const r = check(root, base, { diagnostics: [spanning('src/a.tsx', JSX_AFTER, 2, 3)] });
    assert.equal(r.status, 0, r.out);
  });

  test('falls back to the reported line when the offset points elsewhere', (t) => {
    const { root, base } = repo(
      t,
      { 'client/src/a.tsx': JSX_BEFORE },
      { 'client/src/a.tsx': JSX_AFTER },
    );
    const stale = { ...spanning('src/a.tsx', JSX_AFTER, 1, 5), line: 2 };
    const r = check(root, base, { diagnostics: [stale] });
    assert.equal(r.status, 0, r.out);
  });
});

function narrow(filePath, body, line, word) {
  const offset = body.split('\n').slice(0, line - 1).join('\n').length + (line > 1 ? 1 : 0);
  const at = offset + body.split('\n')[line - 1].indexOf(word);
  return { ...warning(filePath, line), rule: 'jsx-no-jsx-as-prop', offset: at, length: word.length };
}

describe('react-doctor-changed-lines widens to the enclosing node', { concurrency: true }, () => {
  test('fails a warning on the opening tag name when a later attribute line was edited', (t) => {
    const { root, base } = repo(
      t,
      { 'client/src/a.tsx': JSX_BEFORE },
      { 'client/src/a.tsx': JSX_AFTER },
    );
    const r = check(root, base, { diagnostics: [narrow('src/a.tsx', JSX_AFTER, 2, 'Row')] });
    assert.equal(r.status, 1, r.out);
    assert.match(r.out, /src\/a\.tsx:2:1 warning/);
  });

  test('fails a warning with no offset on the first line of an edited multi-line call', (t) => {
    const before = lines('export const x = compute(', '  one,', '  two,', ');');
    const after = lines('export const x = compute(', '  one,', '  three,', ');');
    const { root, base } = repo(t, { 'client/src/a.ts': before }, { 'client/src/a.ts': after });
    const r = check(root, base, { diagnostics: [warning('src/a.ts', 1)] });
    assert.equal(r.status, 1, r.out);
  });

  test('passes a warning on an unchanged element elsewhere in the edited file', (t) => {
    const other = ['export const B = () => (', '  <Row', '    left={null}', '  />', ');'];
    const before = `${JSX_BEFORE}\n${lines(...other)}`;
    const after = `${JSX_AFTER}\n${lines(...other)}`;
    const { root, base } = repo(t, { 'client/src/a.tsx': before }, { 'client/src/a.tsx': after });
    const r = check(root, base, { diagnostics: [narrow('src/a.tsx', after, 9, 'Row')] });
    assert.equal(r.status, 0, r.out);
  });
});

describe('react-doctor-changed-lines survives a deleted file', { concurrency: true }, () => {
  test('passes a stale warning in a file the change deleted', (t) => {
    const { root, base } = repo(
      t,
      { 'client/src/gone.ts': lines(OLD_CHAIN), 'client/src/a.ts': lines(PLAIN) },
      { 'client/src/gone.ts': null, 'client/src/a.ts': lines(PLAIN, 'export const two = 2;') },
    );
    const r = check(root, base, {
      diagnostics: [warning('src/gone.ts', 1), spanning('src/gone.ts', OLD_CHAIN, 1, 1)],
    });
    assert.equal(r.status, 0, r.out);
  });

  test('still fails a new warning beside a deleted file', (t) => {
    const { root, base } = repo(
      t,
      { 'client/src/gone.ts': lines(OLD_CHAIN), 'client/src/a.ts': lines(PLAIN) },
      { 'client/src/gone.ts': null, 'client/src/a.ts': lines(PLAIN, NEW_CHAIN) },
    );
    const r = check(root, base, { diagnostics: [warning('src/a.ts', 2)] });
    assert.equal(r.status, 1, r.out);
    assert.doesNotMatch(r.out, /gone\.ts/);
  });
});

describe(
  'react-doctor-changed-lines --list-new names the warnings to fix',
  { concurrency: true },
  () => {
    test('lists only warnings new versus the base, ignoring line moves', (t) => {
      const { root, base } = repo(
        t,
        { 'client/src/a.ts': lines(PLAIN) },
        { 'client/src/a.ts': lines(PLAIN, PLAIN) },
      );
      const r = listNew(
        root,
        base,
        { diagnostics: [warning('src/a.ts', 9), unused('src/far.ts', 4, 'lonely')] },
        { diagnostics: [warning('src/a.ts', 1)] },
      );
      assert.equal(r.status, 0, r.out);
      assert.match(r.out, /new versus base \w+ \(1, from base report/);
      assert.match(r.out, /^ {2}src\/far\.ts:4 deslop\/unused-export Unused export: `lonely`/m);
      assert.doesNotMatch(r.out, /src\/a\.ts:9/);
      assert.match(r.out, /full report: client\/doctor-report\.json/);
    });

    test('counts a repeated warning as new only when it appears more often', (t) => {
      const { root, base } = repo(t, {}, { 'client/src/a.ts': lines(PLAIN) });
      const r = listNew(
        root,
        base,
        { diagnostics: [warning('src/a.ts', 1), warning('src/a.ts', 5)] },
        { diagnostics: [warning('src/a.ts', 3)] },
      );
      assert.match(r.out, /src\/a\.ts:1 .*\(1 new of 2 alike\)/);
    });

    test('matches a warning in a renamed file to its base path', (t) => {
      const { root, base } = repo(t, { 'client/src/before.ts': lines(OLD_CHAIN) }, {});
      git(root, 'mv', 'client/src/before.ts', 'client/src/after.ts');
      git(root, 'commit', '-qm', 'rename');
      const r = listNew(
        root,
        base,
        { diagnostics: [warning('src/after.ts', 1)] },
        { diagnostics: [warning('src/before.ts', 1)] },
      );
      assert.match(r.out, /\(0, from base report/);
      assert.match(r.out, /none: base \w+ already carries every warning/);
    });

    test('lists every error with file:line', (t) => {
      const { root, base } = repo(t, {}, {});
      const error = { ...warning('src/bad.tsx', 12), severity: 'error', rule: 'no-danger' };
      const r = listNew(
        root,
        base,
        { summary: { warningCount: 0 }, diagnostics: [error] },
        { diagnostics: [] },
        { MAX_WARNINGS: '5' },
      );
      assert.match(r.out, /react-doctor errors \(1\)/);
      assert.match(r.out, /src\/bad\.tsx:12 react-doctor\/no-danger/);
      assert.doesNotMatch(r.out, /new versus base/);
    });

    test('skips the listing while warnings sit at or under the ceiling', (t) => {
      const { root, base } = repo(t, {}, {});
      const r = listNew(
        root,
        base,
        { summary: { warningCount: 5 }, diagnostics: [warning('src/a.ts', 1)] },
        { diagnostics: [] },
        { MAX_WARNINGS: '5' },
      );
      assert.equal(r.out, '');
    });

    test('falls back to the warnings in touched files when the base report is missing', (t) => {
      const { root, base } = repo(
        t,
        { 'client/src/a.ts': lines(PLAIN) },
        { 'client/src/a.ts': lines(PLAIN, NEW_CHAIN) },
      );
      const r = listNew(
        root,
        base,
        { diagnostics: [warning('src/a.ts', 1), warning('src/old.ts', 1)] },
        null,
      );
      assert.equal(r.status, 0, r.out);
      assert.match(r.out, /could not get react-doctor warnings for base/);
      assert.match(r.out, /touched \(1\):\n {2}src\/a\.ts:1 react-doctor\/js-combine-iterations/);
      assert.doesNotMatch(r.out, /src\/old\.ts/);
      assert.match(r.out, /npm run --silent doctor:ci/);
    });
  },
);

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
    const r = spawnSync(process.execPath, [script, base, reportPath], {
      cwd: join(root, 'client'),
      encoding: 'utf8',
    });
    assert.notEqual(r.status, 0, r.stdout);
  });

  test('exits 2 when no base ref is given', (t) => {
    const { root } = repo(t, {}, {});
    const r = spawnSync(process.execPath, [script], {
      cwd: join(root, 'client'),
      encoding: 'utf8',
    });
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
