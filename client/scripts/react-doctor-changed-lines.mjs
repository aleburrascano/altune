import { execFileSync } from 'node:child_process';
import { createRequire } from 'node:module';
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  realpathSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from 'node:fs';
import { homedir, tmpdir } from 'node:os';
import { join } from 'node:path';

const DIFF_DETECT_FLAGS = ['-M', '-C', '--find-copies-harder'];
const GIT_MAX_BUFFER = 256 << 20;
const USAGE_EXIT = 2;
const HUNK = /^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@/;
const FILE_HEADER = /^diff --git a\/.* b\/(.*)$/gm;
const LIST_NEW = '--list-new';
const BASE_SCAN_TIMEOUT_MS = Number(process.env.REACT_DOCTOR_BASE_TIMEOUT_MS ?? 240_000);
const FULL_REPORT_HINT =
  'full report: client/doctor-report.json (regenerate with: cd client && npm run --silent doctor:ci > doctor-report.json)';
const SCRIPT_FILE = /\.[cm]?[jt]sx?$/;
const DOCTOR_ARGS = ['--no-score', '--json', '--json-compact'];

function git(args, cwd) {
  return execFileSync('git', ['-c', 'core.quotePath=false', ...args], {
    cwd,
    encoding: 'utf8',
    maxBuffer: GIT_MAX_BUFFER,
    stdio: ['ignore', 'pipe', 'pipe'],
  });
}

function usage(message) {
  console.error(message);
  process.exit(USAGE_EXIT);
}

function resolveBase(rawBase) {
  if (!rawBase || rawBase.startsWith('-')) {
    usage(
      'usage: react-doctor-changed-lines.mjs <base-ref> [doctor-report.json]\n' +
        '       react-doctor-changed-lines.mjs --list-new <base-ref> <doctor-report.json> [base-report.json]',
    );
  }
  try {
    return git(['rev-parse', '--verify', '--end-of-options', `${rawBase}^{commit}`]).trim();
  } catch {
    return usage(`react-doctor-changed-lines.mjs: cannot resolve base ref ${rawBase}`);
  }
}

function prefixOf() {
  return git(['rev-parse', '--show-prefix']).trim();
}

function removedText(patch) {
  const removed = new Set();
  for (const line of patch.split('\n')) {
    if (!line.startsWith('-') || line.startsWith('---')) continue;
    const text = line.slice(1).trim();
    if (text) removed.add(text);
  }
  return removed;
}

function addedLinesOf(block, moved) {
  const added = new Set();
  let next = null;
  for (const line of block.split('\n')) {
    const hunk = HUNK.exec(line);
    if (hunk) {
      next = Number(hunk[1]);
      continue;
    }
    if (next === null || !line.startsWith('+')) continue;
    if (!moved.has(line.slice(1).trim())) added.add(next);
    next += 1;
  }
  return added;
}

function addedLinesByFile(base) {
  const prefix = prefixOf();
  const patch = git([
    'diff',
    '--text',
    '--diff-filter=ACMR',
    ...DIFF_DETECT_FLAGS,
    '-U0',
    base,
    '--',
    ':/',
  ]);
  const moved = removedText(git(['diff', '--text', ...DIFF_DETECT_FLAGS, '-U0', base, '--', ':/']));
  const headers = [...patch.matchAll(FILE_HEADER)];
  const byFile = new Map();
  headers.forEach((header, i) => {
    const file = header[1];
    if (!file.startsWith(prefix)) return;
    const block = patch.slice(header.index, headers[i + 1]?.index ?? patch.length);
    byFile.set(file.slice(prefix.length), addedLinesOf(block, moved));
  });
  return byFile;
}

function renamesSince(base) {
  const prefix = prefixOf();
  const renames = new Map();
  for (const row of git(['diff', '--name-status', '-M', base, '--', ':/']).split('\n')) {
    const [status, from, to] = row.split('\t');
    if (!status?.startsWith('R') || !from.startsWith(prefix) || !to?.startsWith(prefix)) continue;
    renames.set(from.slice(prefix.length), to.slice(prefix.length));
  }
  return renames;
}

function scanReport(cwd) {
  return execFileSync('node_modules/.bin/react-doctor', DOCTOR_ARGS, {
    cwd,
    encoding: 'utf8',
    maxBuffer: GIT_MAX_BUFFER,
    stdio: ['ignore', 'pipe', 'pipe'],
    timeout: BASE_SCAN_TIMEOUT_MS,
  });
}

function parseReport(text) {
  const report = JSON.parse(text);
  if (!Array.isArray(report.diagnostics)) {
    throw new Error(
      `react-doctor report has no diagnostics list: ${report.error?.message ?? 'unknown error'}`,
    );
  }
  return report;
}

function readReport(reportPath) {
  let text;
  try {
    text = reportPath ? readFileSync(reportPath, 'utf8') : scanReport();
  } catch (error) {
    if (!reportPath && typeof error.stdout === 'string') text = error.stdout;
    else throw error;
  }
  return parseReport(text);
}

function lineAt(source, index) {
  let line = 1;
  for (let i = source.indexOf('\n'); i !== -1 && i < index; i = source.indexOf('\n', i + 1))
    line += 1;
  return line;
}

function rangeOf(diagnostic, sources) {
  const single = { start: diagnostic.line, end: diagnostic.line };
  if (!Number.isInteger(diagnostic.offset) || !Number.isInteger(diagnostic.length)) return single;
  const source = sourceOf(diagnostic.filePath, sources);
  if (source === null || lineAt(source, diagnostic.offset) !== diagnostic.line) return single;
  const last = diagnostic.offset + Math.max(diagnostic.length, 1) - 1;
  return { start: diagnostic.line, end: Math.max(diagnostic.line, lineAt(source, last)) };
}

let tsModule = null;

function typescript() {
  tsModule ??= createRequire(import.meta.url)('typescript');
  return tsModule;
}

function isWideningNode(ts, node) {
  return (
    ts.isJsxOpeningElement(node) ||
    ts.isJsxSelfClosingElement(node) ||
    ts.isCallExpression(node) ||
    ts.isNewExpression(node) ||
    ts.isVariableStatement(node) ||
    ts.isExpressionStatement(node) ||
    ts.isReturnStatement(node)
  );
}

function enclosingNodeRange(diagnostic, source, parsed) {
  const ts = typescript();
  const kind = diagnostic.filePath.endsWith('x') ? ts.ScriptKind.TSX : ts.ScriptKind.TS;
  if (!parsed.has(diagnostic.filePath))
    parsed.set(
      diagnostic.filePath,
      ts.createSourceFile(diagnostic.filePath, source, ts.ScriptTarget.Latest, false, kind),
    );
  const file = parsed.get(diagnostic.filePath);
  if (diagnostic.line < 1 || diagnostic.line > file.getLineStarts().length) return null;
  const lineStart = file.getPositionOfLineAndCharacter(diagnostic.line - 1, 0);
  const at = Number.isInteger(diagnostic.offset) ? diagnostic.offset : lineStart;
  let best = null;
  const visit = (node) => {
    if (node.getStart(file) > at || node.end <= at) return;
    if (isWideningNode(ts, node)) {
      const start = file.getLineAndCharacterOfPosition(node.getStart(file)).line + 1;
      if (start === diagnostic.line)
        best = { start, end: file.getLineAndCharacterOfPosition(node.end).line + 1 };
    }
    ts.forEachChild(node, visit);
  };
  visit(file);
  return best;
}

function sourceOf(filePath, sources) {
  if (!sources.has(filePath)) {
    let source = null;
    try {
      source = readFileSync(filePath, 'utf8');
    } catch {
      source = null;
    }
    sources.set(filePath, source);
  }
  return sources.get(filePath);
}

function touches(added, { start, end }) {
  for (let line = start; line <= end; line += 1) if (added.has(line)) return true;
  return false;
}

function onChangedLines(diagnostics, addedByFile) {
  const sources = new Map();
  const parsed = new Map();
  return diagnostics.filter((d) => {
    const added = addedByFile.get(d.filePath);
    if (!added) return false;
    if (touches(added, rangeOf(d, sources))) return true;
    if (!SCRIPT_FILE.test(d.filePath) || !Number.isInteger(d.line)) return false;
    const source = sourceOf(d.filePath, sources);
    const node = source === null ? null : enclosingNodeRange(d, source, parsed);
    return node !== null && touches(added, node);
  });
}

function describe(d) {
  return `${d.filePath}:${d.line} ${d.plugin}/${d.rule} ${d.message}`;
}

function keyOf(d, filePath = d.filePath) {
  return `${filePath}\0${d.plugin}/${d.rule}\0${d.message}`;
}

function warningsOf(report) {
  return report.diagnostics.filter((d) => d.severity !== 'error');
}

function newWarnings(head, base, renames) {
  const baseCounts = new Map();
  for (const d of warningsOf(base)) {
    const key = keyOf(d, renames.get(d.filePath) ?? d.filePath);
    baseCounts.set(key, (baseCounts.get(key) ?? 0) + 1);
  }
  const headGroups = new Map();
  for (const d of warningsOf(head)) {
    const key = keyOf(d);
    headGroups.set(key, [...(headGroups.get(key) ?? []), d]);
  }
  const rows = [];
  for (const [key, group] of headGroups) {
    const fresh = group.length - (baseCounts.get(key) ?? 0);
    if (fresh <= 0) continue;
    const note = fresh < group.length ? ` (${fresh} new of ${group.length} alike)` : '';
    for (const d of group) rows.push({ d, note });
  }
  return rows.sort((a, b) => a.d.filePath.localeCompare(b.d.filePath) || a.d.line - b.d.line);
}

function doctorVersion() {
  try {
    return JSON.parse(readFileSync('node_modules/react-doctor/package.json', 'utf8')).version;
  } catch {
    return 'unknown';
  }
}

function cachePath(base) {
  const root = process.env.XDG_CACHE_HOME || join(homedir(), '.cache');
  return join(root, 'altune-ci', 'react-doctor', `${base}-${doctorVersion()}.json`);
}

function scanBase(base) {
  const cached = cachePath(base);
  if (existsSync(cached))
    return { report: parseReport(readFileSync(cached, 'utf8')), how: 'cached base report' };
  const top = git(['rev-parse', '--show-toplevel']).trim();
  const tree = mkdtempSync(join(tmpdir(), 'react-doctor-base-'));
  try {
    git(['worktree', 'add', '--detach', '-q', tree, base], top);
    const dir = join(tree, prefixOf());
    symlinkSync(realpathSync('node_modules'), join(dir, 'node_modules'));
    let text;
    try {
      text = scanReport(dir);
    } catch (error) {
      if (typeof error.stdout !== 'string' || error.signal) throw error;
      text = error.stdout;
    }
    const report = parseReport(text);
    mkdirSync(join(cached, '..'), { recursive: true });
    writeFileSync(cached, text);
    return { report, how: 'scanned the base' };
  } finally {
    try {
      git(['worktree', 'remove', '--force', tree], top);
    } catch {
      rmSync(tree, { recursive: true, force: true });
      git(['worktree', 'prune'], top);
    }
  }
}

function baseReport(base, basePath) {
  if (basePath) return { report: readReport(basePath), how: `base report ${basePath}` };
  return scanBase(base);
}

function touchedFiles(base) {
  const prefix = prefixOf();
  return new Set(
    git(['diff', '--name-only', '-z', '--diff-filter=ACMR', ...DIFF_DETECT_FLAGS, base, '--', ':/'])
      .split('\0')
      .filter((file) => file.startsWith(prefix))
      .map((file) => file.slice(prefix.length)),
  );
}

function overCeiling(report) {
  const raw = process.env.MAX_WARNINGS;
  if (raw === undefined || raw === '') return true;
  return report.summary?.warningCount > Number(raw);
}

function listNew(args) {
  const base = resolveBase(args[0]);
  if (!args[1])
    usage(
      'usage: react-doctor-changed-lines.mjs --list-new <base-ref> <doctor-report.json> [base-report.json]',
    );
  const head = readReport(args[1]);
  const errors = head.diagnostics.filter((d) => d.severity === 'error');
  if (errors.length > 0) {
    console.log(`react-doctor errors (${errors.length}), each must be fixed:`);
    for (const d of errors) console.log(`  ${describe(d)}`);
  }
  if (!overCeiling(head)) return;
  const short = base.slice(0, 9);
  let found;
  try {
    found = baseReport(base, args[2]);
  } catch (error) {
    const reason = String(
      error.signal ? `scan timed out after ${BASE_SCAN_TIMEOUT_MS}ms` : error.message,
    ).split('\n')[0];
    const touched = touchedFiles(base);
    const rows = warningsOf(head).filter((d) => touched.has(d.filePath));
    console.log(`could not get react-doctor warnings for base ${short} (${reason}).`);
    console.log(`warnings in the files this change touched (${rows.length}):`);
    for (const d of rows) console.log(`  ${describe(d)}`);
    console.log(FULL_REPORT_HINT);
    return;
  }
  const rows = newWarnings(head, found.report, renamesSince(base));
  console.log(
    `react-doctor warnings new versus base ${short} (${rows.length}, from ${found.how}):`,
  );
  for (const { d, note } of rows) console.log(`  ${describe(d)}${note}`);
  if (rows.length === 0)
    console.log(`  none: base ${short} already carries every warning; the ceiling is below main.`);
  console.log(FULL_REPORT_HINT);
}

function main() {
  if (process.argv[2] === LIST_NEW) {
    listNew(process.argv.slice(3));
    return 0;
  }
  const base = resolveBase(process.argv[2]);
  const addedByFile = addedLinesByFile(base);
  if (addedByFile.size === 0) {
    console.log('No changed files to hold to zero react-doctor warnings.');
    return 0;
  }
  const hits = onChangedLines(readReport(process.argv[3]).diagnostics, addedByFile);
  for (const d of hits) {
    console.log(
      `${d.filePath}:${d.line}:${d.column} ${d.severity} ${d.plugin}/${d.rule}: ${d.message}`,
    );
  }
  console.log(`react-doctor diagnostics on changed lines: ${hits.length}`);
  return hits.length > 0 ? 1 : 0;
}

process.exit(main());
