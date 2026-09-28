import { execFileSync } from 'node:child_process';
import { ESLint } from 'eslint';

const DIFF_DETECT_FLAGS = ['-M', '-C', '--find-copies-harder'];

const rawBase = process.argv[2];
if (!rawBase || rawBase.startsWith('-')) {
  console.error('usage: lint-changed-lines.mjs <base-ref>');
  process.exit(2);
}

const git = (args) =>
  execFileSync('git', ['-c', 'core.quotePath=false', ...args], { encoding: 'utf8' });

let base;
try {
  base = git(['rev-parse', '--verify', '--end-of-options', `${rawBase}^{commit}`]).trim();
} catch {
  console.error(`lint-changed-lines.mjs: cannot resolve base ref ${rawBase}`);
  process.exit(2);
}

const changedFiles = (pathspec) =>
  git([
    'diff',
    '--text',
    '--name-only',
    '-z',
    '--diff-filter=ACMR',
    ...DIFF_DETECT_FLAGS,
    '--relative',
    base,
    '--',
    pathspec,
  ])
    .split('\0')
    .filter(Boolean);

const addedLinesByFile = (pathspec) => {
  const files = changedFiles(pathspec);
  if (files.length === 0) return new Map();
  const hunk = /^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@/;
  const hunksOf = (block) => {
    const added = new Set();
    for (const line of block.split('\n')) {
      const m = hunk.exec(line);
      if (!m) continue;
      const start = Number(m[1]);
      const count = m[2] === undefined ? 1 : Number(m[2]);
      for (let n = start; n < start + count; n += 1) added.add(n);
    }
    return added;
  };
  const patch = git([
    'diff',
    '--text',
    '--diff-filter=ACMR',
    ...DIFF_DETECT_FLAGS,
    '-U0',
    '--relative',
    base,
    '--',
    pathspec,
  ]);
  const blocks = patch.split(/^diff --git .*$/m).slice(1);
  const byFile = new Map();
  files.forEach((file, i) => byFile.set(file, hunksOf(blocks[i] ?? '')));
  return byFile;
};

const files = changedFiles('src').filter(
  (f) => /\.(ts|tsx)$/.test(f) && !f.includes('/__tests__/'),
);
const addedByFile = addedLinesByFile('src');

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
