import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import ts from 'typescript';

import { stripComments } from '../strip-comments.mjs';

const scriptsDir = dirname(dirname(fileURLToPath(import.meta.url)));
const mobileRoot = dirname(scriptsDir);
const repoRoot = dirname(dirname(mobileRoot));
const cliPath = join(scriptsDir, 'strip-comments.mjs');

function scriptKindForFileName(fileName) {
  if (/\.tsx$/.test(fileName) || /\.jsx$/.test(fileName)) return ts.ScriptKind.TSX;
  if (/\.ts$/.test(fileName)) return ts.ScriptKind.TS;
  return ts.ScriptKind.JS;
}

function isEmptyJsxExpressionContainer(node) {
  return ts.isJsxExpression(node) && !node.expression;
}

function renderedJsxText(rawText) {
  return rawText.replace(/\s+/g, ' ').trim();
}

function leafTokens(sourceFile, { dropEmptyJsxContainers = false } = {}) {
  const tokens = [];
  const visit = (node) => {
    if (dropEmptyJsxContainers && isEmptyJsxExpressionContainer(node)) return;
    if (node.kind === ts.SyntaxKind.JSDoc) return;
    const children = node.getChildren(sourceFile);
    if (children.length === 0) {
      tokens.push(node);
      return;
    }
    for (const child of children) visit(child);
  };
  visit(sourceFile);
  tokens.push(sourceFile.endOfFileToken);
  return tokens;
}

function tokenSignature(sourceFile, token) {
  if (token.kind === ts.SyntaxKind.JsxText) {
    return `JsxText:${renderedJsxText(token.getText(sourceFile))}`;
  }
  return `${token.kind}:${token.getText(sourceFile)}`;
}

function parse(text, fileName) {
  return ts.createSourceFile(
    fileName,
    text,
    ts.ScriptTarget.Latest,
    true,
    scriptKindForFileName(fileName),
  );
}

function tokenSignatures(text, fileName, options) {
  const sourceFile = parse(text, fileName);
  return leafTokens(sourceFile, options).map((token) => tokenSignature(sourceFile, token));
}

function nodeKinds(sourceFile, { dropEmptyJsxContainers = false } = {}) {
  const kinds = [];
  const visit = (node) => {
    if (dropEmptyJsxContainers && isEmptyJsxExpressionContainer(node)) return;
    if (node.kind === ts.SyntaxKind.JSDoc) return;
    kinds.push(node.kind);
    node.forEachChild(visit);
  };
  sourceFile.forEachChild(visit);
  return kinds;
}

function nodeKindSignatures(text, fileName, options) {
  return nodeKinds(parse(text, fileName), options);
}

function parseDiagnosticCount(text, fileName) {
  const sourceFile = ts.createSourceFile(
    fileName,
    text,
    ts.ScriptTarget.Latest,
    false,
    scriptKindForFileName(fileName),
  );
  return (sourceFile.parseDiagnostics ?? []).length;
}

const KEPT_DIRECTIVE_PATTERNS = [
  /^\/\/\/\s*<reference\b/,
  /^\/\/\s*@ts-(?:expect-error|ignore|nocheck)\b/,
  /^\/\/\s*eslint-disable/,
  /^\/\*\s*eslint-disable/,
];

function isKeptDirective(commentText) {
  return KEPT_DIRECTIVE_PATTERNS.some((pattern) => pattern.test(commentText));
}

function remainingComments(text, fileName) {
  const sourceFile = parse(text, fileName);
  const seen = new Set();
  const comments = [];
  const collectFromToken = (token) => {
    if (token.kind === ts.SyntaxKind.JsxText) return;
    const leading = ts.getLeadingCommentRanges(text, token.getFullStart()) ?? [];
    const trailing = ts.getTrailingCommentRanges(text, token.getEnd()) ?? [];
    for (const range of [...leading, ...trailing]) {
      const key = `${range.pos}:${range.end}`;
      if (seen.has(key)) continue;
      seen.add(key);
      comments.push(text.slice(range.pos, range.end));
    }
  };
  const visit = (node) => {
    const children = node.getChildren(sourceFile);
    if (children.length === 0) {
      collectFromToken(node);
      return;
    }
    for (const child of children) visit(child);
  };
  visit(sourceFile);
  collectFromToken(sourceFile.endOfFileToken);
  return comments.filter((comment) => !isKeptDirective(comment));
}

test('strips a block comment while leaving a look-alike string literal and matching JS-only stripping', () => {
  const source = 'const s = "// not a comment"; /* gone */ f();';

  const stripped = stripComments(source, 'x.ts');

  assert.ok(stripped.includes('"// not a comment"'));
  assert.ok(!stripped.includes('/* gone */'));
  assert.deepStrictEqual(
    tokenSignatures(stripped, 'x.ts'),
    tokenSignatures('const s = "// not a comment";\nf();', 'x.ts'),
  );
});

test('keeps look-alike comments inside literals and JSX text, removes an empty JSX comment container', () => {
  const source = [
    'const s = "// not a comment";',
    'const t = `template // also not`;',
    "const u = 'https://x.dev';",
    'const r = /\\/\\//;',
    'function Paragraph() {',
    '  return <p>// hi</p>;',
    '}',
    '// @ts-expect-error',
    'const v: number = 1;',
    'function Widget() {',
    '  return <div>{/* x */}<span>ok</span></div>;',
    '}',
  ].join('\n');

  const stripped = stripComments(source, 'x.tsx');

  assert.ok(stripped.includes('"// not a comment"'));
  assert.ok(stripped.includes('`template // also not`'));
  assert.ok(stripped.includes("'https://x.dev'"));
  assert.ok(stripped.includes('/\\/\\//'));
  assert.ok(stripped.includes('<p>// hi</p>'));
  assert.ok(stripped.includes('// @ts-expect-error'));
  assert.ok(!stripped.includes('/* x */'));

  const strippedSourceFile = parse(stripped, 'x.tsx');
  let emptyJsxContainerCount = 0;
  const countEmptyJsxContainers = (node) => {
    if (isEmptyJsxExpressionContainer(node)) emptyJsxContainerCount += 1;
    node.forEachChild(countEmptyJsxContainers);
  };
  strippedSourceFile.forEachChild(countEmptyJsxContainers);
  assert.equal(emptyJsxContainerCount, 0);
});

test('keeps @ts-expect-error only when { all: true } is not requested', () => {
  const source = '// @ts-expect-error\nconst v: number = 1;\n';

  assert.ok(stripComments(source, 'x.ts').includes('// @ts-expect-error'));
  assert.ok(!stripComments(source, 'x.ts', { all: true }).includes('// @ts-expect-error'));
});

test('every tracked source file under apps/mobile and services/overseer/web strips to the same syntax tree', () => {
  const trackedFiles = execFileSync(
    'git',
    ['-C', repoRoot, 'ls-files', 'apps/mobile', 'services/overseer/web'],
    { encoding: 'utf8' },
  )
    .split('\n')
    .filter(Boolean)
    .filter((relativePath) => /\.(ts|tsx|js|jsx|mjs|cjs)$/.test(relativePath))
    .filter((relativePath) => !relativePath.split('/').includes('node_modules'));

  assert.ok(trackedFiles.length > 0);

  for (const relativePath of trackedFiles) {
    const fullPath = join(repoRoot, relativePath);
    const original = readFileSync(fullPath, 'utf8');

    const stripped = stripComments(original, fullPath);

    assert.equal(
      parseDiagnosticCount(stripped, fullPath),
      0,
      `${relativePath} no longer reparses cleanly`,
    );
    assert.deepStrictEqual(
      tokenSignatures(stripped, fullPath),
      tokenSignatures(original, fullPath, { dropEmptyJsxContainers: true }),
      `${relativePath} syntax tree changed`,
    );
    assert.deepStrictEqual(
      nodeKindSignatures(stripped, fullPath),
      nodeKindSignatures(original, fullPath, { dropEmptyJsxContainers: true }),
      `${relativePath} syntax tree node kinds changed`,
    );
    assert.deepStrictEqual(
      remainingComments(stripped, fullPath),
      [],
      `${relativePath} still has a comment`,
    );
  }
});

function makeFixtureDir() {
  const dir = mkdtempSync(join(tmpdir(), 'strip-comments-fixture-'));
  return dir;
}

test('CLI rewrites a mobile fixture, runs prettier with the mobile config, and prints the count', () => {
  const fixtureDir = makeFixtureDir();
  try {
    writeFileSync(
      join(fixtureDir, 'package.json'),
      JSON.stringify({
        prettier: {
          singleQuote: true,
          printWidth: 100,
          trailingComma: 'all',
        },
      }),
    );
    writeFileSync(
      join(fixtureDir, 'widget.ts'),
      [
        '// leading comment',
        'export function add(a: number, b: number) {',
        '  return a + b; /* sum */',
        '}',
        '',
      ].join('\n'),
    );

    const output = execFileSync('node', [cliPath, join(fixtureDir, 'widget.ts')], {
      cwd: mobileRoot,
      encoding: 'utf8',
    });

    assert.match(output, /^stripped \d+ comments in 1 files$/m);

    const rewritten = readFileSync(join(fixtureDir, 'widget.ts'), 'utf8');
    assert.ok(!rewritten.includes('leading comment'));
    assert.ok(!rewritten.includes('/* sum */'));
    assert.ok(rewritten.endsWith('\n'));
    assert.ok(rewritten.includes('return a + b;'));
  } finally {
    rmSync(fixtureDir, { recursive: true, force: true });
  }
});

test('CLI rewrites an overseer-web fixture without prettier', () => {
  const fixtureDir = makeFixtureDir();
  try {
    writeFileSync(
      join(fixtureDir, 'widget.ts'),
      [
        '// leading comment',
        'export function add(a:number,b:number){',
        '  return a+b; /* sum */',
        '}',
        '',
      ].join('\n'),
    );

    const output = execFileSync('node', [cliPath, join(fixtureDir, 'widget.ts')], {
      cwd: mobileRoot,
      encoding: 'utf8',
    });

    assert.match(output, /^stripped \d+ comments in 1 files$/m);

    const rewritten = readFileSync(join(fixtureDir, 'widget.ts'), 'utf8');
    assert.ok(!rewritten.includes('leading comment'));
    assert.ok(!rewritten.includes('/* sum */'));
    assert.ok(rewritten.includes('export function add(a:number,b:number){'));
  } finally {
    rmSync(fixtureDir, { recursive: true, force: true });
  }
});

test('CLI leaves a file untouched and exits 1 when it would not reparse cleanly', () => {
  const fixtureDir = makeFixtureDir();
  try {
    const brokenFile = join(fixtureDir, 'broken.ts');
    writeFileSync(brokenFile, 'const s = "// not a comment"; /* gone */ f(;\n');
    const originalText = readFileSync(brokenFile, 'utf8');

    assert.throws(() =>
      execFileSync('node', [cliPath, brokenFile], { cwd: mobileRoot, encoding: 'utf8' }),
    );

    assert.equal(readFileSync(brokenFile, 'utf8'), originalText);
  } finally {
    rmSync(fixtureDir, { recursive: true, force: true });
  }
});

test('CLI exits 2 with no path argument', () => {
  assert.throws(() => execFileSync('node', [cliPath], { cwd: mobileRoot, encoding: 'utf8' }));
});

test('keeps comment look-alikes inside single-quoted strings and template substitutions, removes a real comment inside a substitution', () => {
  const source =
    "const a = '// x /* y */';\nconst b = `a ${'//' + \"/*\"} b ${`/* ${'//'} */`} c`;\nconst c = `a ${/* gone */ d} e`;\n";

  const stripped = stripComments(source, 'x.ts');

  assert.ok(stripped.includes("'// x /* y */'"));
  assert.ok(stripped.includes("`a ${'//' + \"/*\"} b ${`/* ${'//'} */`} c`"));
  assert.ok(!stripped.includes('gone'));
  assert.deepStrictEqual(
    tokenSignatures(stripped, 'x.ts'),
    tokenSignatures(
      "const a = '// x /* y */';\nconst b = `a ${'//' + \"/*\"} b ${`/* ${'//'} */`} c`;\nconst c = `a ${d} e`;\n",
      'x.ts',
    ),
  );
});

test('keeps regex literals that contain slash-star, a slash class and escaped double slashes', () => {
  const source =
    'const r = /\\/*x/;\nconst q = /[/*]/g;\nconst z = /a\\/\\/b/;\nconst w = /[//]/;\n';

  assert.equal(stripComments(source, 'x.js'), source);
});

test('keeps JSX attribute strings and JSX text that look like comments', () => {
  const source =
    "const e = <a href=\"https://x.dev\" title='/* t */' data={'//'}>/* not */ text // also</a>;\n";

  assert.equal(stripComments(source, 'x.tsx'), source);
});

test('removing a comment between two slashes never turns them into a line comment', () => {
  const source = 'const a = x/**//y;\nf();\n';

  const stripped = stripComments(source, 'x.ts');

  assert.deepStrictEqual(
    tokenSignatures(stripped, 'x.ts'),
    tokenSignatures('const a = x / y;\nf();\n', 'x.ts'),
  );
});

test('a line comment after return keeps the line break, so return still returns nothing', () => {
  const source = 'function f() {\n  return // x\n  value;\n}\n';

  const stripped = stripComments(source, 'x.ts');

  assert.deepStrictEqual(
    tokenSignatures(stripped, 'x.ts'),
    tokenSignatures('function f() {\n  return\n  value;\n}\n', 'x.ts'),
  );
  const returnStatement = parse(stripped, 'x.ts').statements[0].body.statements[0];
  assert.equal(returnStatement.kind, ts.SyntaxKind.ReturnStatement);
  assert.equal(returnStatement.expression, undefined);
});

test('a multi-line block comment after return keeps the line break, so return still returns nothing', () => {
  const source = 'function f() {\n  return /* a\n  b */ value;\n}\n';
  const stripped = stripComments(source, 'x.ts');
  const returnStatement = parse(stripped, 'x.ts').statements[0].body.statements[0];
  assert.equal(returnStatement.expression?.getText(), undefined);
});

test('a multi-line block comment after yield keeps the line break, so yield still yields nothing', () => {
  const source = 'function* g() {\n  yield /*\n  */ 1;\n}\n';
  const stripped = stripComments(source, 'x.ts');
  const yieldExpression = parse(stripped, 'x.ts').statements[0].body.statements[0].expression;
  assert.equal(yieldExpression.expression, undefined);
});

test('removing a comment between two minus or two plus signs never fuses them into a decrement or increment', () => {
  assert.deepStrictEqual(
    tokenSignatures(stripComments('const a = b -/**/-c;\n', 'x.ts'), 'x.ts'),
    tokenSignatures('const a = b - -c;\n', 'x.ts'),
  );
  assert.deepStrictEqual(
    tokenSignatures(stripComments('const a = b +/**/+c;\n', 'x.ts'), 'x.ts'),
    tokenSignatures('const a = b + +c;\n', 'x.ts'),
  );
});

test('a multi-line block comment before a prefix increment keeps the line break', () => {
  const stripped = stripComments('let a = 1\nlet b = a /*\n*/ ++c\n', 'x.js');
  assert.equal(parseDiagnosticCount(stripped, 'x.js'), 0);
  assert.equal(parse(stripped, 'x.js').statements.length, 3);
});

test('keeps a block-form @ts-expect-error by default', () => {
  const source = "/* @ts-expect-error */\nconst v: number = '';\n";
  assert.equal(stripComments(source, 'x.ts'), source);
});

test('a line comment ending a line before an opening paren or bracket keeps the line break', () => {
  const source = 'const a = b // c\n(d)\nconst e = f // g\n[0]\n';

  const stripped = stripComments(source, 'x.js');

  assert.deepStrictEqual(
    tokenSignatures(stripped, 'x.js'),
    tokenSignatures('const a = b\n(d)\nconst e = f\n[0]\n', 'x.js'),
  );
  assert.equal(stripped.split('\n').length, source.split('\n').length);
});

test('removes a slash-star-slash comment and a nested-looking block comment without touching the code after them', () => {
  assert.equal(stripComments('/*/ */\nconst a = 1;\n', 'x.ts'), 'const a = 1;\n');
  assert.equal(stripComments('/* /* */\nconst a = 1;\n', 'x.ts'), 'const a = 1;\n');
});

test('strips trailing comments in a CRLF file and keeps its CRLF line endings and string contents', () => {
  const source = "const a = 1; // t\r\nconst c = '//';\r\n";

  assert.equal(stripComments(source, 'x.ts'), "const a = 1;\r\nconst c = '//';\r\n");
});

test('a comment alone on its own CRLF line is removed entirely, carriage return included', () => {
  const source = '// head\r\nconst a = 1;\r\n/* b */\r\nconst c = 2;\r\n';

  assert.equal(stripComments(source, 'x.ts'), 'const a = 1;\r\nconst c = 2;\r\n');
});

test('keeps a shebang line with and without all', () => {
  const source = '#!/usr/bin/env node\n// c\nconst a = 1;\n';

  assert.equal(stripComments(source, 'x.mjs'), '#!/usr/bin/env node\nconst a = 1;\n');
  assert.equal(
    stripComments(source, 'x.mjs', { all: true }),
    '#!/usr/bin/env node\nconst a = 1;\n',
  );
});

test('keeps reference, ts-ignore, ts-nocheck and eslint-disable directives by default and strips them all with all', () => {
  const source = [
    '/// <reference types="node" />',
    '// @ts-nocheck',
    '// @ts-ignore',
    'const a = 1;',
    '/* eslint-disable no-console */',
    '// eslint-disable-next-line no-x',
    'const b = 2; // eslint-disable-line no-y',
    '// plain',
    '',
  ].join('\n');

  assert.equal(
    stripComments(source, 'x.ts'),
    [
      '/// <reference types="node" />',
      '// @ts-nocheck',
      '// @ts-ignore',
      'const a = 1;',
      '/* eslint-disable no-console */',
      '// eslint-disable-next-line no-x',
      'const b = 2; // eslint-disable-line no-y',
      '',
    ].join('\n'),
  );
  assert.equal(stripComments(source, 'x.ts', { all: true }), 'const a = 1;\nconst b = 2;\n');
});

test('keeps eslint-enable paired with eslint-disable by default and strips both with all', () => {
  const source = [
    '/* eslint-disable no-console */',
    'const a = 1;',
    '/* eslint-enable no-console */',
    '',
  ].join('\n');

  assert.equal(stripComments(source, 'x.ts'), source);
  assert.equal(stripComments(source, 'x.ts', { all: true }), 'const a = 1;\n');
});

function runCli(args) {
  try {
    const stdout = execFileSync('node', [cliPath, ...args], {
      cwd: mobileRoot,
      encoding: 'utf8',
      stdio: 'pipe',
    });
    return { status: 0, stdout, stderr: '' };
  } catch (error) {
    return { status: error.status, stdout: error.stdout, stderr: error.stderr };
  }
}

test('CLI walks a directory for every JS and TS extension and skips build, vendor and native directories', () => {
  const fixtureDir = makeFixtureDir();
  try {
    const skipped = ['node_modules', 'dist', 'build', '.expo', 'ios', 'android'];
    for (const name of skipped) {
      mkdirSync(join(fixtureDir, name));
      writeFileSync(join(fixtureDir, name, 'k.ts'), '// keep\nexport const a = 1;\n');
    }
    mkdirSync(join(fixtureDir, 'sub'));
    const extensions = ['ts', 'tsx', 'js', 'jsx', 'mjs', 'cjs'];
    for (const extension of extensions) {
      writeFileSync(join(fixtureDir, 'sub', `f.${extension}`), '// c\nexport const a = 1;\n');
    }
    writeFileSync(join(fixtureDir, 'notes.md'), '# md // c\n');

    const result = runCli([fixtureDir]);

    assert.equal(result.status, 0);
    assert.match(result.stdout, /^stripped 6 comments in 6 files$/m);
    for (const extension of extensions) {
      assert.equal(
        readFileSync(join(fixtureDir, 'sub', `f.${extension}`), 'utf8'),
        'export const a = 1;\n',
      );
    }
    for (const name of skipped) {
      assert.equal(
        readFileSync(join(fixtureDir, name, 'k.ts'), 'utf8'),
        '// keep\nexport const a = 1;\n',
      );
    }
    assert.equal(readFileSync(join(fixtureDir, 'notes.md'), 'utf8'), '# md // c\n');
  } finally {
    rmSync(fixtureDir, { recursive: true, force: true });
  }
});

test('CLI names the file that would not reparse, leaves it untouched, still rewrites the rest and exits 1', () => {
  const fixtureDir = makeFixtureDir();
  try {
    const brokenFile = join(fixtureDir, 'broken.ts');
    const goodFile = join(fixtureDir, 'good.ts');
    writeFileSync(brokenFile, 'const s = f(;\n// c\n');
    writeFileSync(goodFile, '// c\nexport const a = 1;\n');

    const result = runCli([brokenFile, goodFile]);

    assert.equal(result.status, 1);
    assert.ok(`${result.stdout}${result.stderr}`.includes(brokenFile));
    assert.equal(readFileSync(brokenFile, 'utf8'), 'const s = f(;\n// c\n');
    assert.equal(readFileSync(goodFile, 'utf8'), 'export const a = 1;\n');
  } finally {
    rmSync(fixtureDir, { recursive: true, force: true });
  }
});

test('CLI keeps directives by default and strips them with --all', () => {
  const fixtureDir = makeFixtureDir();
  try {
    const file = join(fixtureDir, 'd.ts');
    writeFileSync(file, '// @ts-ignore\nconst z = 1;\n');

    assert.equal(runCli([file]).status, 0);
    assert.equal(readFileSync(file, 'utf8'), '// @ts-ignore\nconst z = 1;\n');

    assert.equal(runCli(['--all', file]).status, 0);
    assert.equal(readFileSync(file, 'utf8'), 'const z = 1;\n');
  } finally {
    rmSync(fixtureDir, { recursive: true, force: true });
  }
});

test('CLI exits with status 2 and prints usage when given no path', () => {
  const result = runCli([]);

  assert.equal(result.status, 2);
  assert.match(`${result.stdout}${result.stderr}`, /usage/i);
});

test('CLI exits with status 2 and prints usage for an unknown flag, no stack trace', () => {
  const result = runCli(['--bogus', 'x.ts']);

  assert.equal(result.status, 2);
  assert.match(`${result.stdout}${result.stderr}`, /usage/i);
  assert.ok(!`${result.stdout}${result.stderr}`.includes('at Object'));
});

test('CLI exits with status 2 and prints usage for a missing path, no stack trace', () => {
  const fixtureDir = makeFixtureDir();
  try {
    const result = runCli([join(fixtureDir, 'does-not-exist.ts')]);

    assert.equal(result.status, 2);
    assert.match(`${result.stdout}${result.stderr}`, /usage/i);
    assert.ok(!`${result.stdout}${result.stderr}`.includes('at Object'));
  } finally {
    rmSync(fixtureDir, { recursive: true, force: true });
  }
});
