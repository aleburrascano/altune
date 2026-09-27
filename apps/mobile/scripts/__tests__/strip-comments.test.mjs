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
  return ts.createSourceFile(fileName, text, ts.ScriptTarget.Latest, true, scriptKindForFileName(fileName));
}

function tokenSignatures(text, fileName, options) {
  const sourceFile = parse(text, fileName);
  return leafTokens(sourceFile, options).map((token) => tokenSignature(sourceFile, token));
}

function parseDiagnosticCount(text, fileName) {
  const sourceFile = ts.createSourceFile(fileName, text, ts.ScriptTarget.Latest, false, scriptKindForFileName(fileName));
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

    assert.equal(parseDiagnosticCount(stripped, fullPath), 0, `${relativePath} no longer reparses cleanly`);
    assert.deepStrictEqual(
      tokenSignatures(stripped, fullPath),
      tokenSignatures(original, fullPath, { dropEmptyJsxContainers: true }),
      `${relativePath} syntax tree changed`,
    );
    assert.deepStrictEqual(remainingComments(stripped, fullPath), [], `${relativePath} still has a comment`);
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
      ['// leading comment', 'export function add(a: number, b: number) {', '  return a + b; /* sum */', '}', ''].join(
        '\n',
      ),
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
    assert.ok(rewritten.includes("return a + b;"));
  } finally {
    rmSync(fixtureDir, { recursive: true, force: true });
  }
});

test('CLI rewrites an overseer-web fixture without prettier', () => {
  const fixtureDir = makeFixtureDir();
  try {
    writeFileSync(
      join(fixtureDir, 'widget.ts'),
      ['// leading comment', 'export function add(a:number,b:number){', '  return a+b; /* sum */', '}', ''].join(
        '\n',
      ),
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

    assert.throws(() => execFileSync('node', [cliPath, brokenFile], { cwd: mobileRoot, encoding: 'utf8' }));

    assert.equal(readFileSync(brokenFile, 'utf8'), originalText);
  } finally {
    rmSync(fixtureDir, { recursive: true, force: true });
  }
});

test('CLI exits 2 with no path argument', () => {
  assert.throws(() => execFileSync('node', [cliPath], { cwd: mobileRoot, encoding: 'utf8' }));
});
