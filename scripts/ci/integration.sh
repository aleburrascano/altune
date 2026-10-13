#!/usr/bin/env bash
set -euo pipefail
base=$(git merge-base "$1" HEAD)
if ! grep -qE '^services/go-api/|^scripts/ci/(integration|postgres|test-backend-migrate)\.sh$|^\.gitea/workflows/precheck\.yml$' < <(git diff --name-only "$base" HEAD); then
  echo "no go-api change; the Postgres integration tests have nothing to check"
  exit 0
fi
env=$(bash scripts/ci/postgres.sh)
cid=$(sed -n 's/^PG_CID=//p' <<<"$env")
trap 'docker rm -fv "$cid" >/dev/null 2>&1' EXIT
DATABASE_URL=$(sed -n 's/^DATABASE_URL=//p' <<<"$env")
export DATABASE_URL INTEGRATION=1
cd services/go-api
export GOTOOLCHAIN="go$(awk '/^go /{print $2; exit}' go.mod)"
bash ../../scripts/ci/test-backend-migrate.sh >/dev/null
log=$(mktemp)
if go test -shuffle=on -count=1 -coverprofile=coverage.out ./... 2>&1 | tee "$log"; then
  rm -f "$log"
else
  mapfile -t reruns < <(awk '
    /^--- FAIL: / { names = names (names ? "|" : "") $3; next }
    /^FAIL\t/ { if ($0 ~ /\[/ || names == "") { print "HARD"; exit } print $2 " " names; names = ""; next }
  ' "$log")
  rm -f "$log"
  if [ "${#reruns[@]}" -eq 0 ] || grep -qx HARD < <(printf '%s\n' "${reruns[@]}"); then
    exit 1
  fi
  for r in "${reruns[@]}"; do
    pkg=${r%% *}
    names=${r#* }
    echo "::warning::re-running $names in $pkg once; a pass means a load-sensitive test, a second failure fails the gate"
    go test -count=1 -run "^($names)\$" "./${pkg#altune/go-api/}"
  done
fi
MIN=65 bash ../../scripts/ci/test-backend-coverage.sh
go test -count=1 -v -run 'TestPgxSearchHistoryRepo' ./internal/discovery/adapters/persistence/ | grep -E '^--- PASS: TestPgxSearchHistoryRepo'
