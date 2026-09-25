#!/usr/bin/env bash
cd "$(dirname "$0")" || exit 1
th=$PWD/test-home.mjs
fail=0
repo=$(mktemp -d); trap 'rm -rf "$repo"' EXIT
cd "$repo" || exit 1
git init -q && git config user.email t@t && git config user.name t
mkdir -p svc/sched app/lib/__tests__ app/ui e2e
touch svc/sched/scheduler.go svc/sched/scheduler_test.go svc/sched/search.go svc/sched/search_endpoints.go svc/sched/search_endpoints_test.go svc/sched/fresh.go svc/sched/drain.go svc/sched/drain_edge_test.go
touch app/lib/pinnedStore.ts app/lib/__tests__/pinnedStore.test.ts app/ui/Button.tsx app/ui/Button.test.tsx app/ui/security.panel.tsx
printf 'export async function apiFetch() {}\n' > app/lib/client.ts
git add -A && git commit -qm base && git update-ref refs/remotes/origin/main HEAD

t() {
  local want=$1 desc=$2; shift 2
  git checkout -q -B case origin/main && git clean -qfd
  for f in "$@"; do mkdir -p "$(dirname "$f")"; : > "$f"; done
  out=$(node "$th" origin/main 2>&1); got=$?
  [ "$got" = "$want" ] || { echo "FAIL want $want got $got: $desc"; echo "$out" | sed 's/^/    /'; fail=1; }
}
t 0 "no test files added"
t 1 "a per-bug Go test file for a unit with one" svc/sched/scheduler_backpressure_test.go
t 1 "a per-bug TS test file in __tests__" app/lib/__tests__/pinnedStore.expiry.test.ts
t 1 "a colocated TS test beside a unit tested in __tests__" app/lib/pinnedStore.sync.test.ts
t 1 "a _more file" app/ui/Button.more.test.tsx
t 0 "the first test file for a unit" svc/sched/fresh_test.go
t 1 "a unit's first test file named after a scenario" svc/sched/fresh_edge_test.go
t 1 "a TS unit's first test file named after a scenario" app/ui/security.panel.pageFailure.test.tsx
t 0 "the longest source prefix owns the file, not a shorter one" svc/sched/search_test.go
t 0 "an integration file beside a unit test" svc/sched/scheduler_integration_test.go
t 0 "an internal-package test file" svc/sched/scheduler_internal_test.go
t 1 "an internal-package file named after a scenario" svc/sched/stream_health_internal_test.go
t 1 "a property file named after a scenario" app/lib/__tests__/pinnedStore.expiry.property.test.ts
t 0 "a property test file" app/lib/__tests__/pinnedStore.property.test.ts
t 0 "e2e trees are not judged" e2e/pinnedStore.test.ts
t 1 "a Go test file named after the bug" svc/sched/kinds_cap_test.go
t 1 "a TS test file named after the bug" app/lib/__tests__/signOutInFlightRepair.test.ts
t 0 "a helpers file is cross-cutting" svc/sched/helpers_test.go
t 0 "a contract test is cross-cutting" app/lib/__tests__/crossSurfaceContract.test.ts
t 0 "a dotted source name is the unit" app/ui/security.panel.test.tsx
t 0 "an exported function is a unit" app/lib/__tests__/apiFetch.test.ts
t 1 "a scenario file for an exported function that has tests" app/lib/__tests__/apiFetch.authDeadline.test.ts app/lib/__tests__/apiFetch.test.ts
t 0 "a contract file does not count as the unit's home" svc/sched/fresh_error_contract_test.go svc/sched/fresh_test.go
t 0 "a unit's canonical test file beside an older scenario file" svc/sched/drain_test.go
t 1 "another scenario file beside an older scenario file" svc/sched/drain_more_test.go
t 1 "two new scenario test files for a new unit, neither named after it" svc/sched/pool.go svc/sched/pool_a_test.go svc/sched/pool_b_test.go
t 0 "a real unit whose name ends in contract is still judged" svc/sched/backcontract.go svc/sched/backcontract_test.go
t 1 "a scenario file for a unit ending in contract" svc/sched/backcontract.go svc/sched/backcontract_test.go svc/sched/backcontract_x_test.go
t 0 "a new unit and its test in one change" app/lib/offlineQueue.ts app/lib/__tests__/offlineQueue.test.ts
t 1 "two new test files for one new unit" svc/sched/queue.go svc/sched/queue_test.go svc/sched/queue_drain_test.go
t 0 "a named integration test needs no source file" svc/sched/pipeline_integration_test.go

git checkout -q -B tag origin/main && git clean -qfd
printf '//go:build integration\n\npackage sched\n' > svc/sched/scheduler_db_test.go
node "$th" origin/main >/dev/null 2>&1 || { echo "FAIL an integration-tagged file is its own kind"; fail=1; }

git checkout -q -B gather origin/main && git clean -qfd
git rm -q app/lib/__tests__/pinnedStore.test.ts && : > app/lib/pinnedStore.test.ts
node "$th" origin/main >/dev/null 2>&1 || { echo "FAIL a unit's tests gathered into a new home, the old files deleted, pass"; fail=1; }
git reset -q --hard && git clean -qfd

git checkout -q -B renamed origin/main && git clean -qfd
git mv svc/sched/search_endpoints_test.go svc/sched/search_endpoints_timeout_test.go
node "$th" origin/main >/dev/null 2>&1 && { echo "FAIL a rename into a scenario name is judged as an added file"; fail=1; }
git reset -q --hard && git clean -qfd

git checkout -q -B newexport origin/main && git clean -qfd
printf 'export function fetchThing() {}\n' > app/lib/things.ts && : > app/lib/__tests__/fetchThing.test.ts
node "$th" origin/main >/dev/null 2>&1 || { echo "FAIL an exported function added in the same change is a unit"; fail=1; }
git reset -q --hard && git clean -qfd

git checkout -q -B committed origin/main && git clean -qfd
: > svc/sched/scheduler_fix_test.go && git add -A && git commit -qm fix
out=$(node "$th" origin/main 2>&1)
[ $? = 1 ] && grep -q 'svc/sched/scheduler_test.go' <<<"$out" || { echo "FAIL a committed per-bug file names its home: $out"; fail=1; }

[ $fail = 0 ] && echo "test-home.test.sh: all cases pass"
exit $fail
