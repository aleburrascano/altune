#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/client-mutate.sh
real_client=$(git rev-parse --show-toplevel)/client
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

eval "$(bash "$real_client/../scripts/worktree-deps.sh" client)" || { echo "FAIL  client-mutate needs the client's node_modules, and scripts/worktree-deps.sh could not provide them"; exit 1; }

repo=$work/repo
client=$repo/client
mkdir -p "$client"
git init -qb main "$repo"
commit() { git -C "$repo" add -A && git -C "$repo" -c user.email=t@example.com -c user.name=t commit -qm "$1"; }
write() { mkdir -p "$(dirname "$client/$1")" && cat >"$client/$1"; }

ln -s "$real_client/node_modules" "$client/node_modules"
cp "$real_client/jest.mutate.config.js" "$real_client/stryker.config.json" "$client/"
printf 'node_modules\n' >"$repo/.gitignore"
write babel.config.js <<'JS'
module.exports = { presets: ['@babel/preset-typescript'], plugins: ['@babel/plugin-transform-modules-commonjs'] };
JS
write jest.config.js <<'JS'
const WEB_TESTS = '\\.web\\.integration\\.test\\.ts$';
const project = (displayName, extensions, testMatch, testPathIgnorePatterns) => ({
  displayName,
  rootDir: __dirname,
  testEnvironment: 'node',
  moduleFileExtensions: extensions,
  testMatch,
  testPathIgnorePatterns: ['/node_modules/', ...testPathIgnorePatterns],
  moduleNameMapper: { '^@shared/(.*)$': '<rootDir>/src/shared/$1', '^@features/(.*)$': '<rootDir>/src/features/$1' },
});
module.exports = {
  projects: [
    project('native', ['ts', 'js'], ['**/__tests__/**/*.test.ts'], [WEB_TESTS]),
    project('web', ['web.ts', 'ts', 'js'], ['**/__tests__/**/*.web.integration.test.ts'], []),
  ],
};
JS
write src/shared/clamp/index.ts <<'TS'
export { adult } from './core/adult';
TS
write src/shared/clamp/core/adult.ts <<'TS'
export function adult(age: number): boolean {
  return age >= 18;
}
TS
write src/shared/clamp/__tests__/adult.test.ts <<'TS'
import { adult } from '@shared/clamp';

test('adult', () => {
  expect(adult(17)).toBe(false);
  expect(adult(18)).toBe(true);
});
TS
write src/shared/label/index.ts <<'TS'
export { label } from './core/label';
TS
write src/shared/label/core/label.ts <<'TS'
export function label(n: number): string {
  const prefix = 'n';
  if (n > 100) return 'big';
  const suffix = '!';
  return prefix + suffix;
}
TS
write src/shared/label/__tests__/label.test.ts <<'TS'
import { label } from '@shared/label';

test('label', () => {
  expect(label(5)).toBe('n!');
});
TS
write src/features/price/index.ts <<'TS'
export { price } from './core/price';
TS
write src/features/price/core/price.ts <<'TS'
export function price(cents: number): string {
  return `${cents}c`;
}
TS
write src/features/price/core/price.web.ts <<'TS'
export function price(cents: number): string {
  return cents >= 100 ? `$${cents / 100}` : `${cents}c`;
}
TS
write src/features/price/__tests__/price.web.integration.test.ts <<'TS'
import { price } from '@features/price';

test('price on the web', () => {
  expect(price(99)).toBe('99c');
  expect(price(100)).toBe('$1');
});
TS
commit base
base=$(git -C "$repo" rev-parse HEAD)

run_env=()
expect() {
  local name=$1 want=$2 pattern=$3 rc
  out=$(cd "$repo" && env "${run_env[@]}" bash "$script" "$base" 2>&1); rc=$?
  if [ "$rc" != "$want" ] || ! grep -qE "$pattern" <<<"$out"; then
    echo "FAIL  $name: exit $rc (want $want), output:"; echo "$out" | sed 's/^/      /'; fail=1
  else
    echo "ok    $name"
  fi
}

out=$(bash "$script" 2>&1); [ $? = 2 ] && grep -q usage <<<"$out" && echo "ok    no arguments prints usage" || { echo "FAIL  no arguments"; fail=1; }

sed -i 's/expect(adult(18)).toBe(true);/expect(adult(18)).toBe(true);\n  expect(adult(40)).toBe(true);/' "$client/src/shared/clamp/__tests__/adult.test.ts"
commit tests-only
expect "a change to tests only has nothing to mutate" 0 '0 changed files, killed 0, survived 0'

sed -i 's/age >= 18/18 <= age/' "$client/src/shared/clamp/core/adult.ts"
commit tested
expect "a changed line the tests cover passes" 0 '1 changed files, killed [1-9][0-9]*, survived 0'

sed -i -e "s/const prefix = 'n';/const prefix = 'm';/" -e "s/const suffix = '!';/const suffix = '?';/" "$client/src/shared/label/core/label.ts"
sed -i "s/'n!'/'m?'/" "$client/src/shared/label/__tests__/label.test.ts"
commit two-lines-apart
expect "an unchanged line between two changed lines is not mutated" 0 '2 changed files, killed [1-9][0-9]*, survived 0'

sed -i "s/const suffix = '?';/const suffix = n > 3 ? '?' : '!';/" "$client/src/shared/label/core/label.ts"
commit later-line-mutable
expect "a mutant the tests miss fails and names its line" 1 'client/src/shared/label/core/label.ts:4:[0-9]+ Survived'
if grep -q 'label.ts:3:' <<<"$out"; then
  echo "FAIL  the unchanged line between them stays unmutated:"; echo "$out" | sed 's/^/      /'; fail=1
else
  echo "ok    the unchanged line between them stays unmutated"
fi
base=$(git -C "$repo" rev-parse HEAD)

cat >>"$client/src/shared/clamp/core/adult.ts" <<'TS'

export function senior(age: number): boolean {
  return age > 64;
}
TS
commit untested
expect "a changed line no test covers fails and names the line" 1 'client/src/shared/clamp/core/adult.ts:[0-9]+:[0-9]+ (Survived|NoCoverage)'
base=$(git -C "$repo" rev-parse HEAD)

sed -i 's/cents >= 100/cents > 99/' "$client/src/features/price/core/price.web.ts"
commit web-file
expect "a web file is mutated against the web jest project, which runs its tests" 0 '1 changed files, killed [1-9][0-9]*, survived 0'
base=$(git -C "$repo" rev-parse HEAD)

write src/shared/clamp/core/minor.ts <<'TS'
export function minor(age: number): boolean {
  return age < 18;
}
TS
printf "export { minor } from './core/minor';\n" >>"$client/src/shared/clamp/index.ts"
write src/shared/label/__tests__/minor.test.ts <<'TS'
import { minor } from '@shared/clamp';

test('minor', () => {
  expect(minor(17)).toBe(true);
  expect(minor(18)).toBe(false);
});
TS
commit tested-only-by-another-module
expect "another module's tests cannot kill a module's mutant" 1 'client/src/shared/clamp/core/minor.ts:[0-9]+:[0-9]+ (Survived|NoCoverage)'
base=$(git -C "$repo" rev-parse HEAD)

git -C "$repo" mv client/src/shared/clamp/core/minor.ts client/src/shared/clamp/core/young.ts
sed -i 's#./core/minor#./core/young#' "$client/src/shared/clamp/index.ts"
commit renamed
expect "a file renamed with no edits mutates nothing" 0 'killed 0, survived 0'
base=$(git -C "$repo" rev-parse HEAD)

sed -i '1i export type Age = number;' "$client/src/shared/clamp/core/young.ts"
commit type-only
expect "a changed line Stryker has no mutant for passes" 0 'killed 0, survived 0'
base=$(git -C "$repo" rev-parse HEAD)

write src/shared/lonely/index.ts <<'TS'
export { alone } from './core/alone';
TS
write src/shared/lonely/core/alone.ts <<'TS'
export function alone(n: number): boolean {
  return n === 1;
}
TS
commit untested-module
expect "a changed file no test reaches names its module's test folder" 1 'no test in src/shared/lonely runs src/shared/lonely/core/alone.ts'
git -C "$repo" rm -rq client/src/shared/lonely
commit drop-untested-module
base=$(git -C "$repo" rev-parse HEAD)

incremental=$work/incremental
run_env=(CLIENT_MUTATE_INCREMENTAL_DIR="$incremental")
sed -i 's/18 <= age/age > 17/' "$client/src/shared/clamp/core/adult.ts"
commit incremental-first-run
expect "an incremental run with nothing kept tests every mutant" 0 'killed [1-9][0-9]*, survived 0, reused 0'
expect "a run on the same client files reuses the kept results" 0 'killed [1-9][0-9]*, survived 0, reused [1-9][0-9]*'
sed -i '1i const UNUSED_SETUP = 1;' "$client/src/shared/clamp/__tests__/adult.test.ts"
commit incremental-test-setup-changed
expect "a run after a test file's setup changed reuses nothing" 0 'killed [1-9][0-9]*, survived 0, reused 0'
base=$(git -C "$repo" rev-parse HEAD)

write src/shared/slow/index.ts <<'TS'
export { grade } from './core/grade';
TS
{
  echo 'export function grade(n: number): string {'
  for i in $(seq 1 15); do echo "  if (n === $i) return 'g$i';"; done
  echo "  return 'none';"
  echo '}'
} | write src/shared/slow/core/grade.ts
write src/shared/slow/__tests__/grade.test.ts <<'TS'
import { openSync } from 'node:fs';

import { grade } from '@shared/slow';

const BLOCK_MS = 600_000;

type StrykerGlobal = { __stryker__?: { activeMutant?: string } };

function firstMutantRun(): boolean {
  try {
    openSync(`${process.env.FIRST_MUTANT_MARKER}`, 'wx');
    return true;
  } catch {
    return false;
  }
}

test('grade', () => {
  const underMutation = (globalThis as StrykerGlobal).__stryker__?.activeMutant !== undefined;
  if (underMutation && !firstMutantRun()) {
    Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, BLOCK_MS);
  }
  for (let n = 1; n <= 15; n += 1) expect(grade(n)).toBe(`g${n}`);
  expect(grade(16)).toBe('none');
});
TS
jq '. + { timeoutMS: 900000 }' "$client/stryker.config.json" >"$work/stryker.config.json"
cp "$client/stryker.config.json" "$work/stryker.config.json.real"
cp "$work/stryker.config.json" "$client/stryker.config.json"
commit slow
run_env=(CLIENT_MUTATE_INCREMENTAL_DIR="$incremental" CLIENT_MUTATE_TIME_LIMIT_SECONDS=120 FIRST_MUTANT_MARKER="$work/first-mutant")
expect "a run past its time limit fails and says the finished mutants are kept" 1 'passed its 120s time limit on the native jest project, and the mutants it finished are kept in .*run it again without pushing'
stopped_run=$out
kept=$(jq '[.files[].mutants[] | select(.status != "RuntimeError")] | length' "$incremental/stryker-native.json" 2>&1)
if [[ $kept =~ ^[1-9][0-9]*$ ]]; then
  echo "ok    the mutants finished before the time limit are kept"
else
  echo "FAIL  the mutants finished before the time limit are kept: $kept. The stopped run said:"; echo "$stopped_run" | sed 's/^/      /'; fail=1
fi
crashed=$(jq '[.files[].mutants[] | select(.status == "RuntimeError")] | length' "$incremental/stryker-native.json" 2>&1)
if [ "$crashed" = 0 ]; then
  echo "ok    runs the time limit cut short are not kept as results"
else
  echo "FAIL  runs the time limit cut short are not kept as results: $crashed"; fail=1
fi
run_env=(CLIENT_MUTATE_TIME_LIMIT_SECONDS=soon)
expect "a time limit that is not whole seconds is refused" 2 "CLIENT_MUTATE_TIME_LIMIT_SECONDS is 'soon'"
run_env=()
git -C "$repo" rm -rq client/src/shared/slow
cp "$work/stryker.config.json.real" "$client/stryker.config.json"
commit drop-slow
base=$(git -C "$repo" rev-parse HEAD)

sed -i 's/expect(adult(17)).toBe(false);/expect(adult(17)).toBe(true);/' "$client/src/shared/clamp/__tests__/adult.test.ts"
sed -i 's/age > 64/age >= 65/' "$client/src/shared/clamp/core/adult.ts"
commit broken
expect "a failing test stops the run loudly" 1 'Stryker failed on the native jest project'

exit $fail
