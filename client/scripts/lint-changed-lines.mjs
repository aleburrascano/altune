import { execFileSync } from 'node:child_process';
import { ESLint } from 'eslint';
import { addedLinesSince, DIFF_DETECT_FLAGS } from './changed-lines.mjs';

const rawBase = process.argv[2];
if (!rawBase || rawBase.startsWith('-')) {
  console.error('usage: lint-changed-lines.mjs <base-ref>');
  process.exit(2);
}

const git = (args) =>
  execFileSync('git', ['-c', 'core.quotePath=false', ...args], {
    encoding: 'utf8',
    maxBuffer: 256 << 20,
  });

let base;
try {
  base = git(['rev-parse', '--verify', '--end-of-options', `${rawBase}^{commit}`]).trim();
} catch {
  console.error(`lint-changed-lines.mjs: cannot resolve base ref ${rawBase}`);
  process.exit(2);
}

const prefix = git(['rev-parse', '--show-prefix']).trim();

const underPathspec = (pathspec) => (file) => file.startsWith(`${prefix}${pathspec}/`);

const changedFiles = (pathspec) =>
  git([
    'diff',
    '--text',
    '--name-only',
    '-z',
    '--diff-filter=ACMR',
    ...DIFF_DETECT_FLAGS,
    base,
    '--',
    ':/',
  ])
    .split('\0')
    .filter(Boolean)
    .filter(underPathspec(pathspec))
    .map((file) => file.slice(prefix.length));

const files = changedFiles('src').filter((f) => /\.(ts|tsx)$/.test(f));
const addedByFile = addedLinesSince(git, base, prefix, underPathspec('src'));

let total = 0;
if (files.length === 0) {
  console.log('No changed src files to enforce mechanical style on.');
} else {
  console.log('Enforcing mechanical style on added lines of:');
  for (const f of files) console.log(`  ${f}`);

  process.env.ESLINT_DIFF_SCOPED = '1';
  const eslint = new ESLint();
  const results = await eslint.lintFiles(files);

  const onlyAddedLines = (result) => {
    const added = addedByFile.get(result.filePath.replace(`${process.cwd()}/`, '')) ?? new Set();
    const messages = result.messages.filter((m) => added.has(m.line));
    return { ...result, messages, errorCount: messages.length, warningCount: 0 };
  };

  const filtered = results.map(onlyAddedLines).filter((r) => r.messages.length > 0);
  total = filtered.reduce((sum, r) => sum + r.messages.length, 0);

  const formatter = await eslint.loadFormatter('stylish');
  if (total > 0) console.log(await formatter.format(filtered));
  console.log(`new-code mechanical-style violations: ${total}`);
}

process.exit(total > 0 ? 1 : 0);
