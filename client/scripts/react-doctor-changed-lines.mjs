import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';

const DIFF_DETECT_FLAGS = ['-M', '-C', '--find-copies-harder'];
const GIT_MAX_BUFFER = 256 << 20;
const USAGE_EXIT = 2;
const HUNK = /^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@/;
const FILE_HEADER = /^diff --git a\/.* b\/(.*)$/gm;

function git(args) {
  return execFileSync('git', ['-c', 'core.quotePath=false', ...args], {
    encoding: 'utf8',
    maxBuffer: GIT_MAX_BUFFER,
  });
}

function usage(message) {
  console.error(message);
  process.exit(USAGE_EXIT);
}

function resolveBase(rawBase) {
  if (!rawBase || rawBase.startsWith('-')) {
    usage('usage: react-doctor-changed-lines.mjs <base-ref> [doctor-report.json]');
  }
  try {
    return git(['rev-parse', '--verify', '--end-of-options', `${rawBase}^{commit}`]).trim();
  } catch {
    return usage(`react-doctor-changed-lines.mjs: cannot resolve base ref ${rawBase}`);
  }
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
  const prefix = git(['rev-parse', '--show-prefix']).trim();
  const patch = git(['diff', '--text', '--diff-filter=ACMR', ...DIFF_DETECT_FLAGS, '-U0', base, '--', ':/']);
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

function scanReport() {
  return execFileSync('node_modules/.bin/react-doctor', ['--no-score', '--json', '--json-compact'], {
    encoding: 'utf8',
    maxBuffer: GIT_MAX_BUFFER,
  });
}

function readReport(reportPath) {
  let text;
  try {
    text = reportPath ? readFileSync(reportPath, 'utf8') : scanReport();
  } catch (error) {
    if (!reportPath && typeof error.stdout === 'string') text = error.stdout;
    else throw error;
  }
  const report = JSON.parse(text);
  if (!Array.isArray(report.diagnostics)) {
    throw new Error(`react-doctor report has no diagnostics list: ${report.error?.message ?? 'unknown error'}`);
  }
  return report;
}

function onChangedLines(diagnostics, addedByFile) {
  return diagnostics.filter((d) => addedByFile.get(d.filePath)?.has(d.line));
}

function main() {
  const base = resolveBase(process.argv[2]);
  const addedByFile = addedLinesByFile(base);
  if (addedByFile.size === 0) {
    console.log('No changed files to hold to zero react-doctor warnings.');
    return 0;
  }
  const hits = onChangedLines(readReport(process.argv[3]).diagnostics, addedByFile);
  for (const d of hits) {
    console.log(`${d.filePath}:${d.line}:${d.column} ${d.severity} ${d.plugin}/${d.rule}: ${d.message}`);
  }
  console.log(`react-doctor diagnostics on changed lines: ${hits.length}`);
  return hits.length > 0 ? 1 : 0;
}

process.exit(main());
