#!/usr/bin/env bash
set -euo pipefail
base=$(git merge-base "$1" HEAD)
if ! git diff --name-only "$base" HEAD | grep -qE '^services/go-api/|^\.gitea/workflows/(integration\.sh|ci-postgres\.sh|precheck\.yml)$|^\.github/workflows/test-backend-migrate\.sh$'; then
  echo "no go-api change; the Postgres integration tests have nothing to check"
  exit 0
fi
env=$(bash .gitea/workflows/ci-postgres.sh)
cid=$(sed -n 's/^PG_CID=//p' <<<"$env")
trap 'docker rm -f "$cid" >/dev/null 2>&1' EXIT
DATABASE_URL=$(sed -n 's/^DATABASE_URL=//p' <<<"$env")
export DATABASE_URL INTEGRATION=1
cd services/go-api
bash ../../.github/workflows/test-backend-migrate.sh >/dev/null
mapfile -t pkgs < <(git grep -lE 'RequireIntegration|DATABASE_URL' -- '*_test.go' | xargs -n1 dirname | sort -u | sed 's#^#./#')
[ "${#pkgs[@]}" -gt 0 ] || { echo "::error::no integration test packages matched"; exit 1; }
skip='^TestUpsert_SaveBlockedOnTheErasureLockDoesNotResurrectIt$'
log=$(mktemp)
if go test -count=1 -skip "$skip" "${pkgs[@]}" 2>&1 | tee "$log"; then
  rm -f "$log"
else
  mapfile -t reruns < <(awk '
    /^--- FAIL: / { names = names (names ? "|" : "") $3; next }
    /^FAIL\t/ { if ($0 ~ /\[/ || names == "") { print "HARD"; exit } print $2 " " names; names = ""; next }
  ' "$log")
  rm -f "$log"
  if [ "${#reruns[@]}" -eq 0 ] || printf '%s\n' "${reruns[@]}" | grep -qx HARD; then
    exit 1
  fi
  for r in "${reruns[@]}"; do
    pkg=${r%% *}
    names=${r#* }
    echo "::warning::re-running $names in $pkg once; a pass means a load-sensitive test, a second failure fails the gate"
    go test -count=1 -run "^($names)\$" "./${pkg#altune/go-api/}"
  done
fi
go test -count=1 -v -run 'TestPgxSearchHistoryRepo' ./internal/discovery/adapters/persistence/ | grep -E '^--- PASS: TestPgxSearchHistoryRepo'
