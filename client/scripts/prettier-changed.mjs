import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { check, resolveConfig } from 'prettier';

const git = (args) =>
  execFileSync('git', ['-c', 'core.quotePath=false', ...args], {
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
  });

const DEFAULT_BASES = ['gitea/main', 'origin/main'];

function defaultBase() {
  for (const ref of DEFAULT_BASES) {
    try {
      git(['rev-parse', '--verify', '--quiet', `${ref}^{commit}`]);
      return ref;
    } catch {
      continue;
    }
  }
  console.error(
    `prettier-changed.mjs: no base ref given and none of ${DEFAULT_BASES.join(', ')} exists`,
  );
  process.exit(2);
}

const rawBase = process.argv[2] ?? defaultBase();
if (!rawBase || rawBase.startsWith('-')) {
  console.error('usage: prettier-changed.mjs [base-ref] (default: merge-base with gitea/main)');
  process.exit(2);
}

let mergeBase;
try {
  mergeBase = git(['merge-base', '--end-of-options', rawBase, 'HEAD']).trim();
} catch {
  console.error(`prettier-changed.mjs: cannot resolve base ref ${rawBase}`);
  process.exit(2);
}

const changed = git([
  'diff',
  '--name-only',
  '-z',
  '--diff-filter=ACMR',
  '-M',
  '-C',
  '--relative',
  '--cached',
  mergeBase,
  '--',
  'src/*.ts',
  'src/*.tsx',
])
  .split('\0')
  .filter(Boolean);

if (changed.length === 0) {
  console.log('prettier-changed: no changed files');
  process.exit(0);
}

const unformatted = [];
for (const path of changed) {
  const config = await resolveConfig(path);
  const formatted = await check(readFileSync(path, 'utf8'), { ...config, filepath: path });
  if (!formatted) unformatted.push(path);
}

if (unformatted.length > 0) {
  console.error('prettier-changed: not Prettier-clean:');
  for (const path of unformatted) console.error(path);
  process.exit(1);
}
console.log(`prettier-changed: ${changed.length} changed file(s) clean`);
