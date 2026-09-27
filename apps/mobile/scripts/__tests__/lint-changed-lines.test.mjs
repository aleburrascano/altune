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

  test('flags a comment added to a path with a space', async (t) => {
    const { root, base } = repo(t, {}, {
      '__tests__/sp ace/b.test.ts': 'export const a = 1;\n// note\n',
    });
    const r = await lint(root, base);
    assert.equal(r.status, 1, r.stdout + r.stderr);
    assert.ok(r.stdout.includes('__tests__/sp ace/b.test.ts:2 '), r.stdout);
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

  test('flags a new comment in a brand-new file', async (t) => {
    const { root, base } = repo(t, {}, {
      '__tests__/brand-new.test.ts': '// brand new\nexport const a = 1;\n',
    });
    const r = await lint(root, base);
    assert.equal(r.status, 1, r.stdout + r.stderr);
    assert.ok(r.stdout.includes('__tests__/brand-new.test.ts:1 '), r.stdout);
  });
});

describe('lint-changed-lines comment gate on hostile paths and bases', { concurrency: true }, () => {
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
    for (const bad of [`${base}..HEAD`, 'HEAD...HEAD', 'HEAD..HEAD', 'HEAD^{tree}', 'HEAD:__tests__/a.test.ts']) {
      const r = await lint(root, bad);
      assert.equal(r.status, 2, `${bad}: ${r.stdout}${r.stderr}`);
    }
  });

  test('flags a comment added to a file renamed in the same change', async (t) => {
    const body = 'export const a = 1;\nexport const b = 2;\nexport const c = 3;\nexport const d = 4;\nexport const e = 5;\n';
    const { root, base } = repo(t, { '__tests__/old name.test.ts': body }, {});
    rmSync(join(root, '__tests__/old name.test.ts'));
    write(root, { '__tests__/new name.test.ts': `${body}// note\n` });
    git(root, 'add', '-A');
    git(root, 'commit', '-qm', 'rename with edit');
    const r = await lint(root, base);
    assert.equal(r.status, 1, r.stdout + r.stderr);
    assert.ok(r.stdout.includes('__tests__/new name.test.ts:6 '), r.stdout);
  });

  test('flags a comment added to a copy of an unchanged file', async (t) => {
    const body = '// kept\nexport const a = 1;\nexport const b = 2;\nexport const c = 3;\nexport const d = 4;\n';
    const { root, base } = repo(t, { '__tests__/source.test.ts': body }, {
      '__tests__/copy.test.ts': `${body}// added\n`,
    });
    const r = await lint(root, base);
    assert.equal(r.status, 1, r.stdout + r.stderr);
    assert.ok(r.stdout.includes('__tests__/copy.test.ts:6 '), r.stdout);
    assert.ok(!r.stdout.includes('__tests__/copy.test.ts:1 '), r.stdout);
  });

  test('does not flag the comments of an unchanged file copied as is', async (t) => {
    const body = '// kept\nexport const a = 1;\nexport const b = 2;\nexport const c = 3;\nexport const d = 4;\n';
    const { root, base } = repo(t, { '__tests__/source.test.ts': body }, { '__tests__/copy.test.ts': body });
    const r = await lint(root, base);
    assert.equal(r.status, 0, r.stdout + r.stderr);
  });

  test('flags a new file whose comment repeats the text of an unrelated base comment', async (t) => {
    const { root, base } = repo(t, { '__tests__/old.test.ts': '// note\nexport const a = 1;\n' }, {
      '__tests__/other.test.ts': 'export const z = 99;\n// note\nexport const y = 98;\n',
    });
    const r = await lint(root, base);
    assert.equal(r.status, 1, r.stdout + r.stderr);
    assert.ok(r.stdout.includes('__tests__/other.test.ts:2 '), r.stdout);
  });

  test('passes a change that only deletes a file, even one with a space in its path', async (t) => {
    const { root, base } = repo(t, { '__tests__/go ne.test.ts': '// old\nexport const a = 1;\n' }, {});
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

  test('flags a comment against a base with no common ancestor', async (t) => {
    const { root } = repo(t, {}, {});
    const branch = git(root, 'rev-parse', '--abbrev-ref', 'HEAD');
    git(root, 'checkout', '-q', '--orphan', 'unrelated');
    git(root, 'rm', '-rqf', '.');
    write(root, { 'jest/keep.js': 'module.exports = 0;\n' });
    git(root, 'add', '-A');
    git(root, 'commit', '-qm', 'unrelated base');
    const orphan = git(root, 'rev-parse', 'HEAD');
    git(root, 'checkout', '-q', branch);
    write(root, { '__tests__/a.test.ts': 'export const a = 1;\n// note\n' });
    git(root, 'add', '-A');
    git(root, 'commit', '-qm', 'change');
    const r = await lint(root, orphan);
    assert.equal(r.status, 1, r.stdout + r.stderr);
    assert.ok(r.stdout.includes('__tests__/a.test.ts:2 '), r.stdout);
  });

  test('flags a comment added to a tracked file but not yet committed', async (t) => {
    const { root, base } = repo(t, { '__tests__/a.test.ts': 'export const a = 1;\n' }, {});
    write(root, { '__tests__/a.test.ts': 'export const a = 1;\n// note\n' });
    const r = await lint(root, base);
    assert.equal(r.status, 1, r.stdout + r.stderr);
    assert.ok(r.stdout.includes('__tests__/a.test.ts:2 '), r.stdout);
  });

  for (const name of ['ünï cödé', 'ta\tb', 'q"uo', 'back\\slash', 'new\nline']) {
    test(`flags a comment added to a file named ${JSON.stringify(name)}`, async (t) => {
      const { root, base } = repo(t, {}, { [`__tests__/${name}.test.ts`]: 'export const a = 1;\n// note\n' });
      const r = await lint(root, base);
      assert.equal(r.status, 1, r.stdout + r.stderr);
    });
  }

  test('names a non-ASCII path as it is on disk', async (t) => {
    const { root, base } = repo(t, {}, { '__tests__/ünï cödé.test.ts': 'export const a = 1;\n// note\n' });
    const r = await lint(root, base);
    assert.equal(r.status, 1, r.stdout + r.stderr);
    assert.ok(r.stdout.includes('__tests__/ünï cödé.test.ts:2 '), r.stdout);
  });

  test('flags a comment added to a file that .gitattributes marks binary', async (t) => {
    const { root, base } = repo(t, { '__tests__/a.test.ts': 'export const a = 1;\n' }, {
      '.gitattributes': '*.ts binary\n',
      '__tests__/a.test.ts': 'export const a = 1;\n// note\n',
    });
    const r = await lint(root, base);
    assert.equal(r.status, 1, r.stdout + r.stderr);
  });

  test('flags a comment added to a file with a NUL byte', async (t) => {
    const { root, base } = repo(t, {}, {
      '__tests__/nul.test.ts': 'export const a = "\0";\n// note\n',
    });
    const r = await lint(root, base);
    assert.equal(r.status, 1, r.stdout + r.stderr);
  });
});
