import { spawnSync } from 'node:child_process';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const USAGE =
  'usage: fallow-changed.mjs BASE_REF   checks the module boundaries, part boundaries and duplicated code in the client files changed since BASE_REF';
const ANY_DUPLICATION = '0.0001';
const client = join(dirname(fileURLToPath(import.meta.url)), '..');

const args = process.argv.slice(2);
if (args.length !== 1 || args[0].startsWith('-')) {
  console.error(USAGE);
  process.exit(2);
}
const base = args[0];

const resolved = spawnSync('git', ['rev-parse', '--verify', '--quiet', `${base}^{commit}`], {
  cwd: client,
  stdio: 'ignore',
});
if (resolved.status !== 0) {
  console.error(
    `fallow-changed: cannot resolve ${base} to a commit, and fallow would then check the whole tree`,
  );
  process.exit(2);
}

const CHECKS = [
  ['dead-code', '-c', 'fallow/modules.json', '--boundary-violations'],
  ['dead-code', '-c', 'fallow/parts.json', '--boundary-violations'],
  ['dupes', '--threshold', ANY_DUPLICATION],
];

let broken = false;
for (const check of CHECKS) {
  const run = spawnSync(
    join(client, 'node_modules/.bin/fallow'),
    [...check, '--changed-since', base],
    { cwd: client, stdio: 'inherit' },
  );
  if (run.status === 1) {
    broken = true;
  } else if (run.status !== 0) {
    console.error(
      `fallow-changed: fallow ${check.join(' ')} could not run (${run.error?.message ?? `exit ${run.status ?? run.signal}`})`,
    );
    process.exit(3);
  }
}

if (broken) {
  console.error(
    'fallow-changed: the findings above break the client module rules. Read client/src/README.md.',
  );
  process.exit(1);
}
