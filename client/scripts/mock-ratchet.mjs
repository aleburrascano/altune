import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  diffAgainstBaseline,
  exitWithBaselineReport,
  readBaselineLines,
} from './ratchet-baseline.mjs';

const scriptRoot = path.join(path.dirname(fileURLToPath(import.meta.url)), '..');
const mobileRoot = process.env.MOCK_RATCHET_ROOT ?? scriptRoot;
const baselinePath =
  process.env.MOCK_RATCHET_BASELINE_FILE ??
  path.join(mobileRoot, 'baselines', 'mock-ratchet.baseline');

const SCAN_DIRS = ['src', 'jest'];
const JEST_TEST_DIRS = ['__tests__', 'scripts'];
const JEST_TEST_FILE = /(^|\/)__tests__\/(.+\/)?[^/]+\.test\.tsx?$/;
const SOURCE_EXTENSIONS = new Set(['.ts', '.tsx', '.js', '.mjs']);
const SKIP_DIRS = new Set(['node_modules']);
const OWN_PREFIXES = ['./', '../', '@shared/', '@features/', '@/', '@testing/'];
const MOCK_CALL = /jest\.(?:mock|doMock)\(\s*(['"`])([^'"`]*)\1/g;
const UPDATE_BASELINE_HINT =
  'the count went down: run `cd client && node scripts/mock-ratchet.mjs --write` to update the baseline';

function sourceFiles(dir) {
  if (!fs.existsSync(dir)) return [];
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) return SKIP_DIRS.has(entry.name) ? [] : sourceFiles(full);
    return SOURCE_EXTENSIONS.has(path.extname(entry.name)) ? [full] : [];
  });
}

export function isOwnModule(specifier) {
  return OWN_PREFIXES.some((prefix) => specifier.startsWith(prefix));
}

export function ownMocksIn(source) {
  return [...source.matchAll(MOCK_CALL)]
    .map((match) => match[2])
    .filter((specifier) => isOwnModule(specifier));
}

function relativeTo(root, file) {
  return path.relative(root, file).split(path.sep).join('/');
}

function jestTestFiles(root) {
  return JEST_TEST_DIRS.flatMap((dir) => sourceFiles(path.join(root, dir))).filter((file) =>
    JEST_TEST_FILE.test(relativeTo(root, file)),
  );
}

export function findMockLines(root = mobileRoot) {
  const files = [
    ...SCAN_DIRS.flatMap((dir) => sourceFiles(path.join(root, dir))),
    ...jestTestFiles(root),
  ];
  return files
    .flatMap((file) => {
      const relative = relativeTo(root, file);
      return ownMocksIn(fs.readFileSync(file, 'utf8')).map(
        (specifier) => `${relative} ${specifier}`,
      );
    })
    .sort();
}

export function readBaseline(file = baselinePath) {
  if (!fs.existsSync(file)) return [];
  return readBaselineLines(file);
}

function writeBaseline(found, added) {
  if (added.length > 0) {
    for (const line of added) console.log(`new violation: ${line}`);
    console.log('mock-ratchet: --write only removes fixed lines; remove the new mocks first');
    process.exit(1);
  }
  fs.mkdirSync(path.dirname(baselinePath), { recursive: true });
  fs.writeFileSync(baselinePath, found.map((line) => `${line}\n`).join(''));
  console.log(`mock-ratchet: wrote ${found.length} own-module mocks to the baseline`);
}

function main() {
  const found = findMockLines();
  const baseline = readBaseline();
  const diff = diffAgainstBaseline(found, baseline);

  if (process.argv.includes('--write')) {
    const hasBaselineToProtect = fs.existsSync(baselinePath);
    writeBaseline(found, hasBaselineToProtect ? diff.added : []);
    return;
  }

  console.log(`own-module mocks: ${found.length} (baseline ${baseline.length})`);
  exitWithBaselineReport(diff, UPDATE_BASELINE_HINT);
}

if (import.meta.url === `file://${process.argv[1]}`) {
  main();
}
