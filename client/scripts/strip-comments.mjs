import { readFileSync, writeFileSync, statSync, readdirSync } from 'node:fs';
import { extname, join } from 'node:path';
import { pathToFileURL } from 'node:url';
import ts from 'typescript';
import * as prettier from 'prettier';

const WALKED_EXTENSIONS = new Set(['.ts', '.tsx', '.js', '.jsx', '.mjs', '.cjs']);
const SKIPPED_DIRECTORY_NAMES = new Set([
  'node_modules',
  'dist',
  'build',
  '.expo',
  'ios',
  'android',
]);

const REFERENCE_DIRECTIVE_PATTERN = /^\/\/\/\s*<reference\b/;
const DIRECTIVE_BODY_PATTERNS = [
  /^@ts-(?:expect-error|ignore|nocheck)\b/,
  /^eslint-disable/,
  /^eslint-enable/,
];

function scriptKindForFileName(fileName) {
  if (/\.tsx$/.test(fileName) || /\.jsx$/.test(fileName)) return ts.ScriptKind.TSX;
  if (/\.ts$/.test(fileName)) return ts.ScriptKind.TS;
  return ts.ScriptKind.JS;
}

function directiveBody(commentText) {
  if (commentText.startsWith('///')) return null;
  if (commentText.startsWith('//')) return commentText.slice(2).replace(/^\s+/, '');
  if (commentText.startsWith('/*')) return commentText.slice(2, -2).replace(/^\s+/, '');
  return null;
}

function isDirectiveComment(commentText) {
  if (REFERENCE_DIRECTIVE_PATTERN.test(commentText)) return true;
  const body = directiveBody(commentText);
  return body !== null && DIRECTIVE_BODY_PATTERNS.some((pattern) => pattern.test(body));
}

function isWordCharacter(character) {
  return character !== undefined && /[\w$]/.test(character);
}

function tokensWouldFuse(charBefore, charAfter) {
  if (charBefore === undefined || charAfter === undefined) return false;
  const scanner = ts.createScanner(ts.ScriptTarget.Latest, false);
  scanner.setText(charBefore + charAfter);
  scanner.scan();
  return scanner.getTextPos() === 2;
}

function spanForRemovableComment(text, range) {
  const lineStart = text.lastIndexOf('\n', range.pos - 1) + 1;
  const nextNewline = text.indexOf('\n', range.end);
  const lineEnd = nextNewline === -1 ? text.length : nextNewline;
  const beforeOnLine = text.slice(lineStart, range.pos);
  const afterOnLine = text.slice(range.end, lineEnd);
  const isAloneOnLine = /^[ \t\r]*$/.test(beforeOnLine) && /^[ \t\r]*$/.test(afterOnLine);

  if (isAloneOnLine) {
    const end = nextNewline === -1 ? text.length : nextNewline + 1;
    return { start: lineStart, end, replacement: '' };
  }

  let start = range.pos;
  while (start > lineStart && (text[start - 1] === ' ' || text[start - 1] === '\t')) start -= 1;

  const commentText = text.slice(range.pos, range.end);
  if (commentText.includes('\n')) {
    return { start, end: range.end, replacement: '\n' };
  }

  const charBefore = text[start - 1];
  const charAfter = text[range.end];
  const replacement =
    (isWordCharacter(charBefore) && isWordCharacter(charAfter)) ||
    tokensWouldFuse(charBefore, charAfter)
      ? ' '
      : '';
  return { start, end: range.end, replacement };
}

function collectCommentRanges(sourceFile, text) {
  const leaves = [];
  const removals = [];

  const visit = (node) => {
    if (ts.isJsxExpression(node) && !node.expression) {
      removals.push({ start: node.getStart(sourceFile), end: node.getEnd(), replacement: '' });
      return;
    }
    const children = node.getChildren(sourceFile);
    if (children.length === 0) {
      leaves.push(node);
      return;
    }
    for (const child of children) visit(child);
  };

  visit(sourceFile);
  leaves.push(sourceFile.endOfFileToken);

  const seenPositions = new Set();
  const ranges = [];
  const addRange = (range) => {
    const key = `${range.pos}:${range.end}`;
    if (seenPositions.has(key)) return;
    seenPositions.add(key);
    ranges.push(range);
  };

  for (let index = 0; index < leaves.length; index += 1) {
    const token = leaves[index];
    if (token.kind === ts.SyntaxKind.JsxText) continue;

    const previous = leaves[index - 1];
    const next = leaves[index + 1];

    if (!previous || previous.kind !== ts.SyntaxKind.JsxText) {
      for (const range of ts.getLeadingCommentRanges(text, token.getFullStart()) ?? [])
        addRange(range);
    }
    if (!next || next.kind !== ts.SyntaxKind.JsxText) {
      for (const range of ts.getTrailingCommentRanges(text, token.getEnd()) ?? []) addRange(range);
    }
  }

  return { commentRanges: ranges, emptyJsxContainerRemovals: removals };
}

function reparsesCleanly(fileName, text) {
  const scriptKind = scriptKindForFileName(fileName);
  const sourceFile = ts.createSourceFile(fileName, text, ts.ScriptTarget.Latest, false, scriptKind);
  const diagnostics = sourceFile.parseDiagnostics ?? [];
  return diagnostics.length === 0;
}

function strip(text, fileName, { all = false } = {}) {
  const scriptKind = scriptKindForFileName(fileName);
  const sourceFile = ts.createSourceFile(fileName, text, ts.ScriptTarget.Latest, true, scriptKind);
  const { commentRanges, emptyJsxContainerRemovals } = collectCommentRanges(sourceFile, text);

  const commentRemovals = commentRanges
    .filter((range) => all || !isDirectiveComment(text.slice(range.pos, range.end)))
    .map((range) => spanForRemovableComment(text, range));

  const removals = [...commentRemovals, ...emptyJsxContainerRemovals].sort(
    (a, b) => a.start - b.start,
  );

  let output = '';
  let cursor = 0;
  for (const removal of removals) {
    output += text.slice(cursor, removal.start) + removal.replacement;
    cursor = removal.end;
  }
  output += text.slice(cursor);

  return { text: output, removedCount: commentRemovals.length };
}

export function stripComments(text, fileName, options = {}) {
  return strip(text, fileName, options).text;
}

function collectFiles(startPath) {
  const stat = statSync(startPath);
  if (stat.isFile()) {
    return WALKED_EXTENSIONS.has(extname(startPath)) ? [startPath] : [];
  }
  const files = [];
  for (const entry of readdirSync(startPath, { withFileTypes: true })) {
    if (SKIPPED_DIRECTORY_NAMES.has(entry.name)) continue;
    const fullPath = join(startPath, entry.name);
    if (entry.isDirectory()) {
      files.push(...collectFiles(fullPath));
    } else if (WALKED_EXTENSIONS.has(extname(entry.name))) {
      files.push(fullPath);
    }
  }
  return files;
}

async function formatWithPrettierIfConfigured(filePath, text) {
  const config = await prettier.resolveConfig(filePath);
  if (!config) return text;
  return prettier.format(text, { ...config, filepath: filePath });
}

async function runCli(argv) {
  const unknownFlag = argv.find((argument) => argument.startsWith('--') && argument !== '--all');
  const all = argv.includes('--all');
  const paths = argv.filter((argument) => argument !== '--all');
  if (unknownFlag || paths.length === 0) {
    console.error('usage: strip-comments.mjs [--all] <path>...');
    return 2;
  }

  for (const path of paths) {
    try {
      statSync(path);
    } catch {
      console.error('usage: strip-comments.mjs [--all] <path>...');
      return 2;
    }
  }

  const files = paths.flatMap((path) => collectFiles(path));
  const failedFiles = [];
  let strippedCount = 0;
  let processedFileCount = 0;

  for (const file of files) {
    const originalText = readFileSync(file, 'utf8');
    const { text: strippedText, removedCount } = strip(originalText, file, { all });

    if (!reparsesCleanly(file, strippedText)) {
      failedFiles.push(file);
      continue;
    }

    const formattedText = await formatWithPrettierIfConfigured(file, strippedText);
    writeFileSync(file, formattedText, 'utf8');
    strippedCount += removedCount;
    processedFileCount += 1;
  }

  console.log(`stripped ${strippedCount} comments in ${processedFileCount} files`);
  for (const file of failedFiles) {
    console.error(`did not reparse cleanly after stripping, left untouched: ${file}`);
  }

  return failedFiles.length > 0 ? 1 : 0;
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? '').href) {
  runCli(process.argv.slice(2)).then((code) => process.exit(code));
}
