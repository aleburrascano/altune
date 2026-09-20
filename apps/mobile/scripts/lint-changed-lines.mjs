// Enforce the mechanical style rules (eslint.config.js, gated behind
// ESLINT_DIFF_SCOPED) on ONLY the lines a change added or modified, so a 10-line
// function cap and a banned-name list can hold at the house's real thresholds
// without retroactively failing the 500+ pre-existing long functions and vague
// identifiers that live in files a PR merely touches. A violation blocks only
// when eslint reports it on an added line; pre-existing code stays untouched.
//
// Usage: node scripts/lint-changed-lines.mjs <base-ref>
// Run from apps/mobile. Exits 1 if any new-code violation remains.
import { execFileSync } from 'node:child_process';
import { ESLint } from 'eslint';

const base = process.argv[2];
if (!base) {
  console.error('usage: lint-changed-lines.mjs <base-ref>');
  process.exit(2);
}

const git = (args) => execFileSync('git', args, { encoding: 'utf8' });

const changedSrcFiles = () =>
  git(['diff', '--name-only', '--relative', '--diff-filter=ACMR', base, '--', 'src'])
    .split('\n')
    .filter((f) => /\.(ts|tsx)$/.test(f) && !f.includes('/__tests__/'));

const addedLinesOf = (file) => {
  const added = new Set();
  const hunk = /^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@/;
  for (const line of git(['diff', '-U0', base, '--', file]).split('\n')) {
    const m = hunk.exec(line);
    if (!m) continue;
    const start = Number(m[1]);
    const count = m[2] === undefined ? 1 : Number(m[2]);
    for (let n = start; n < start + count; n += 1) added.add(n);
  }
  return added;
};

const files = changedSrcFiles();
if (files.length === 0) {
  console.log('No changed src files to enforce mechanical style on.');
  process.exit(0);
}
console.log('Enforcing mechanical style on added lines of:');
for (const f of files) console.log(`  ${f}`);

process.env.ESLINT_DIFF_SCOPED = '1';
const eslint = new ESLint();
const results = await eslint.lintFiles(files);

const onlyAddedLines = (result) => {
  const added = addedLinesOf(result.filePath.replace(`${process.cwd()}/`, ''));
  const messages = result.messages.filter((m) => added.has(m.line));
  return { ...result, messages, errorCount: messages.length, warningCount: 0 };
};

const filtered = results.map(onlyAddedLines).filter((r) => r.messages.length > 0);
const total = filtered.reduce((sum, r) => sum + r.messages.length, 0);

const formatter = await eslint.loadFormatter('stylish');
if (total > 0) console.log(await formatter.format(filtered));
console.log(`new-code mechanical-style violations: ${total}`);
process.exit(total > 0 ? 1 : 0);
