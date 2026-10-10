// Counts jest.mock / jest.doMock calls whose target is the project's own code
// (relative paths and the jest.config.js path aliases). The count may only go
// down: a call not in the baseline fails, and so does a baseline line whose
// call is gone. Third-party modules are not counted. No third-party imports:
// this runs before `npm ci`. --write only removes fixed lines: it refuses while a
// new mock exists, unless there is no baseline file yet to protect.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptRoot = path.join(path.dirname(fileURLToPath(import.meta.url)), '..');
const mobileRoot = process.env.MOCK_RATCHET_ROOT ?? scriptRoot;
const baselinePath =
  process.env.MOCK_RATCHET_BASELINE_FILE ?? path.join(mobileRoot, 'mock-ratchet.baseline');

const SCAN_DIRS = ['src', 'jest'];
// The other folders jest's testMatch reaches; only its test files are read there,
// so a node:test file that holds jest.mock text as a fixture is not counted.
const JEST_TEST_DIRS = ['__tests__', 'scripts'];
const JEST_TEST_FILE = /(^|\/)__tests__\/(.+\/)?[^/]+\.test\.tsx?$/;
const SOURCE_EXTENSIONS = new Set(['.ts', '.tsx', '.js', '.mjs']);
const SKIP_DIRS = new Set(['node_modules']);
// Mirrors the moduleNameMapper aliases in client/jest.config.js.
const OWN_PREFIXES = ['./', '../', '@shared/', '@features/', '@/', '@testing/'];
const MOCK_CALL = /jest\.(?:mock|doMock)\(\s*(['"`])([^'"`]*)\1/g;
const UPDATE_BASELINE_HINT =
  'the count went down: run `node scripts/mock-ratchet.mjs --write` in client/ to update the baseline';

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

function writeBaseline(found, added) {
  if (added.length > 0) {
    for (const line of added) console.log(`new violation: ${line}`);
    console.log('mock-ratchet: --write only removes fixed lines; remove the new mocks first');
    process.exit(1);
  }
  fs.writeFileSync(baselinePath, found.map((line) => `${line}\n`).join(''));
  console.log(`mock-ratchet: wrote ${found.length} own-module mocks to the baseline`);
}

function main() {
  const found = findMockLines();
  const baseline = readBaseline();
  const { added, fixed } = diffAgainstBaseline(found, baseline);

  if (process.argv.includes('--write')) {
    const hasBaselineToProtect = fs.existsSync(baselinePath);
    writeBaseline(found, hasBaselineToProtect ? added : []);
    return;
  }

  console.log(`own-module mocks: ${found.length} (baseline ${baseline.length})`);
  for (const line of added) console.log(`new violation: ${line}`);
  for (const line of fixed) console.log(`fixed: remove it from the baseline: ${line}`);
  if (fixed.length > 0 && added.length === 0) console.log(UPDATE_BASELINE_HINT);
  const failed = added.length > 0 || fixed.length > 0;

  process.exit(failed ? 1 : 0);
}

if (import.meta.url === `file://${process.argv[1]}`) {
  main();
}
