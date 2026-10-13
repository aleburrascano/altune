import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { ESLint } from 'eslint';
import { globSync } from 'glob';
import tsParser from '@typescript-eslint/parser';
import { loadRules } from '../eslint/consistency/index.js';

const mobileRoot = path.join(path.dirname(fileURLToPath(import.meta.url)), '..');
const baselinePath =
  process.env.CONSISTENCY_BASELINE_FILE ?? path.join(mobileRoot, 'baselines', 'consistency.baseline');
// The tree the CLI scans; tests point it at a temp copy so they never write under src.
const scanRoot = process.env.CONSISTENCY_ROOT ?? mobileRoot;
const UPDATE_BASELINE_HINT =
  'the count went down: run `cd client && node scripts/consistency-ratchet.mjs --write` to update the baseline';

const tsxParsing = {
  files: ['**/*.{ts,tsx}'],
  languageOptions: {
    parser: tsParser,
    parserOptions: { ecmaFeatures: { jsx: true } },
  },
};

function buildLinter(rule, cwd = mobileRoot) {
  return new ESLint({
    cwd,
    overrideConfigFile: true,
    overrideConfig: [tsxParsing, ...rule.config],
  });
}

function srcFiles() {
  return globSync('src/**/*.{ts,tsx}', {
    cwd: scanRoot,
    ignore: ['**/__tests__/**', '**/*.test.*'],
  });
}

async function lintFiles(rule, files) {
  if (files.length === 0) return [];
  const linter = buildLinter(rule, scanRoot);
  const results = await linter.lintFiles(files);
  return results.flatMap((result) =>
    result.messages.map((message) => ({
      file: path.relative(scanRoot, result.filePath),
      message: message.message,
    })),
  );
}

export function violationLine(rule, violation) {
  return `${rule.id} ${violation.file} ${violation.message.replace(/\s+/g, ' ').trim()}`;
}

export function readBaseline(file = baselinePath) {
  return fs
    .readFileSync(file, 'utf8')
    .split('\n')
    .filter((line) => line !== '');
}

// Multiset difference: a line listed once permits one occurrence.
export function diffAgainstBaseline(found, baseline) {
  const remaining = new Map();
  for (const line of baseline) remaining.set(line, (remaining.get(line) ?? 0) + 1);
  const added = [];
  for (const line of found) {
    const left = remaining.get(line) ?? 0;
    if (left === 0) added.push(line);
    else remaining.set(line, left - 1);
  }
  const fixed = [...remaining].flatMap(([line, count]) => Array(count).fill(line));
  return { added, fixed };
}

export async function lintSource(rule, source, filename) {
  const linter = buildLinter(rule);
  const results = await linter.lintText(source, { filePath: path.join(mobileRoot, filename) });
  return results.flatMap((result) => result.messages);
}

function writeBaseline(found, added) {
  if (added.length > 0) {
    for (const line of added) console.log(`new violation: ${line}`);
    console.log(
      'consistency-ratchet: --write only removes fixed lines; fix the new violations first',
    );
    process.exit(1);
  }
  fs.writeFileSync(baselinePath, found.map((line) => `${line}\n`).join(''));
  console.log(`consistency-ratchet: wrote ${found.length} violations to the baseline`);
}

async function main() {
  const files = srcFiles();
  const rules = loadRules();

  if (rules.length === 0) {
    console.error('consistency-ratchet: no rule modules found in client/eslint/consistency/');
    process.exit(1);
  }
  if (files.length === 0) {
    console.error('consistency-ratchet: no source files matched src/**/*.{ts,tsx}');
    process.exit(1);
  }

  const found = [];
  for (const rule of rules) {
    const violations = await lintFiles(rule, files);
    console.log(`${rule.id}: ${violations.length}`);
    found.push(...violations.map((violation) => violationLine(rule, violation)));
  }

  found.sort();
  const { added, fixed } = diffAgainstBaseline(found, readBaseline());
  if (process.argv.includes('--write')) {
    writeBaseline(found, added);
    return;
  }

  for (const line of added) console.log(`new violation: ${line}`);
  for (const line of fixed) console.log(`fixed: remove it from the baseline: ${line}`);
  if (fixed.length > 0 && added.length === 0) console.log(UPDATE_BASELINE_HINT);
  const failed = added.length > 0 || fixed.length > 0;

  process.exit(failed ? 1 : 0);
}

if (import.meta.url === `file://${process.argv[1]}`) {
  await main();
}
