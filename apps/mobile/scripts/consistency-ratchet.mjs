import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { ESLint } from 'eslint';
import { globSync } from 'glob';
import tsParser from '@typescript-eslint/parser';
import { loadRules } from '../eslint/consistency/index.js';

const mobileRoot = path.join(path.dirname(fileURLToPath(import.meta.url)), '..');

const tsxParsing = {
  files: ['**/*.{ts,tsx}'],
  languageOptions: {
    parser: tsParser,
    parserOptions: { ecmaFeatures: { jsx: true } },
  },
};

function buildLinter(rule) {
  return new ESLint({
    cwd: mobileRoot,
    overrideConfigFile: true,
    overrideConfig: [tsxParsing, ...rule.config],
  });
}

function srcFiles() {
  return globSync('src/**/*.{ts,tsx}', {
    cwd: mobileRoot,
    ignore: ['**/__tests__/**', '**/*.test.*'],
  });
}

async function lintFiles(rule, files) {
  if (files.length === 0) return [];
  const linter = buildLinter(rule);
  const results = await linter.lintFiles(files);
  return results.flatMap((result) =>
    result.messages.map((message) => ({
      file: path.relative(mobileRoot, result.filePath),
      message: message.message,
    })),
  );
}

export async function countViolations(rule, files = srcFiles()) {
  return (await lintFiles(rule, files)).length;
}

export async function lintSource(rule, source, filename) {
  const linter = buildLinter(rule);
  const results = await linter.lintText(source, { filePath: path.join(mobileRoot, filename) });
  return results.flatMap((result) => result.messages);
}

async function main() {
  const files = srcFiles();
  const rules = loadRules();

  if (rules.length === 0) {
    console.error('consistency-ratchet: no rule modules found in apps/mobile/eslint/consistency/');
    process.exit(1);
  }
  if (files.length === 0) {
    console.error('consistency-ratchet: no source files matched src/**/*.{ts,tsx}');
    process.exit(1);
  }

  let failed = false;

  for (const rule of rules) {
    const violations = await lintFiles(rule, files);
    console.log(`${rule.id}: ${violations.length} / baseline ${rule.baseline}`);
    if (violations.length === rule.baseline) continue;

    failed = true;
    for (const violation of violations) {
      console.log(`  ${rule.id} ${violation.file}: ${violation.message}`);
    }
    const direction = violations.length > rule.baseline ? 'fix the new violation' : `lower baseline to ${violations.length}`;
    console.log(`${rule.id}: ${direction} in apps/mobile/eslint/consistency/`);
  }

  process.exit(failed ? 1 : 0);
}

if (import.meta.url === `file://${process.argv[1]}`) {
  await main();
}
