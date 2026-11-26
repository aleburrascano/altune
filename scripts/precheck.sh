#!/usr/bin/env bash
set -uo pipefail

stage=push
case "${1:-}" in
  --lint) stage=commit; shift ;;
esac
lint_only=0
[ $stage = commit ] && lint_only=1

root=$(git rev-parse --show-toplevel) || exit 3
cd "$root" || exit 3
lint_cache="$root/.cache/golangci-lint"
default_base_ref() { git remote get-url gitea >/dev/null 2>&1 && git rev-parse --verify --quiet gitea/main >/dev/null && echo gitea/main || echo origin/main; }
base_ref=${1:-$(default_base_ref)}
base=$(git merge-base "$base_ref" HEAD) || { echo "precheck: no merge base with $base_ref"; exit 3; }

PATH="$PATH:$(go env GOPATH 2>/dev/null)/bin"

untracked() { [ $stage = commit ] || git ls-files --others --exclude-standard; }
if [ $stage = commit ]; then
  base=$(git rev-parse HEAD) || { echo "precheck: no HEAD commit to compare the pending changes with"; exit 3; }
  changed=$(git -c core.quotePath=false diff --name-only --diff-filter=ACMR "$base" | sort -u)
  deleted=$(git -c core.quotePath=false diff --name-only --no-renames --diff-filter=D "$base")
else
  changed=$( { git diff --name-only --diff-filter=ACMR "$base"; untracked; } | sort -u)
  deleted=$(git diff --name-only --no-renames --diff-filter=D "$base")
fi
touches() { grep -qE "$1" <<<"$changed"$'\n'"$deleted"; }

main_tree=$(git worktree list --porcelain | awk '/^worktree /{print $2; exit}')
link_deps() {
  local out
  out=$(bash scripts/worktree-deps.sh "$1") || return 1
  eval "$out"
}

heavy=()
[ -f "$HOME/.claude/bin/heavy.sh" ] && heavy=(bash "$HOME/.claude/bin/heavy.sh")

failed=0
missing=0
log=$(mktemp)

fail_summaries=()
current_step=start
finished=0
record_fail() {
  local name=$1 rc=$2 text=$3 first
  first=$(printf '%s\n' "$text" | sed -E 's/\x1b\[[0-9;]*m//g' | grep -v '^[[:space:]]*$' | head -n 3 | sed 's/^[[:space:]]*/    /')
  [ -n "$first" ] || first="    (no output; exit $rc)"
  fail_summaries+=("FAIL $name (exit $rc):"$'\n'"$first")
}
abort_rc=
announce_abort() {
  local rc=${abort_rc:-$?}
  [ $finished = 1 ] && return
  [ "$rc" = 0 ] && return
  echo "precheck: stopped early with exit $rc during: $current_step"
  [ ${#fail_summaries[@]} -eq 0 ] || printf '%s\n' "${fail_summaries[@]}"
  echo "FAIL precheck itself (exit $rc): died during $current_step"
}
trap 'announce_abort; rm -f "$log"' EXIT
trap 'abort_rc=129; exit 129' HUP
trap 'abort_rc=130; exit 130' INT
trap 'abort_rc=143; exit 143' TERM

last_rc=0
run_check() {
  local show=$1 name=$2 dir=$3; shift 3
  current_step=$name
  (cd "$dir" && "$@") >"$log" 2>&1
  last_rc=$?
  if [ $last_rc = 0 ]; then
    echo "ok    $name"
  else
    echo "FAIL  $name   ($dir: $*)"
    "$show" | sed 's/^/      /'
    record_fail "$name" "$last_rc" "$("$show")"
    failed=1
  fi
  current_step="between steps, after $name"
}

log_tail() { tail -n 40 "$log"; }

check() { run_check log_tail "$@"; }

test_check() {
  [ $lint_only = 1 ] || check "$@"
}

jest_limit=${PRECHECK_JEST_TIMEOUT:-1800}

jest_failures() {
  if [ "$last_rc" = 124 ] || [ "$last_rc" = 137 ]; then
    echo "jest did not exit within ${jest_limit}s and was killed (raise PRECHECK_JEST_TIMEOUT if the suite is just slow)"
  fi
  local names
  names=$(sed -E 's/\x1b\[[0-9;]*m//g' "$log" | grep -E '^[[:space:]]*● ' | grep -vE '● Console$' | sed -E 's/^[[:space:]]+//' | awk '!seen[$0]++')
  if [ -n "$names" ]; then
    echo "failed tests:"
    echo "$names"
    sed -E 's/\x1b\[[0-9;]*m//g' "$log" | grep -E '^(Test Suites|Tests):'
    echo "first failure:"
    sed -E 's/\x1b\[[0-9;]*m//g' "$log" | awk '
      /^[[:space:]]*● / { if (on) exit; if (!/● Console$/) on = 1 }
      on && /^[[:space:]]+at / { next }
      on { print; if (++n >= 25) exit }'
  else
    log_tail
  fi
}

jest_check() {
  [ $lint_only = 1 ] && return 0
  local name=$1 dir=$2; shift 2
  run_check jest_failures "$name" "$dir" "${heavy[@]}" timeout --kill-after=30 "$jest_limit" "$@"
}

need() { command -v "$1" >/dev/null 2>&1 || { echo "SKIP  $2: $1 not installed"; missing=1; return 1; }; }

go_pin() {
  export GOTOOLCHAIN="go$(awk '/^go /{print $2; exit}' "$1/go.mod")"
}

go_pkgs() {
  local m=$1 d
  printf '%s\n%s\n' "$changed" "$deleted" | grep -E "^$m/.*\.go$" | xargs -r -d '\n' -n1 dirname | sort -u \
    | sed "s#^$m#.#" | while read -r d; do
      [ -d "$m/$d" ] || continue
      (cd "$m" && go list "$d") >/dev/null 2>&1 && echo "$d"
    done
}

go_affected() {
  local m=$1 mod files
  mod=$(cd "$m" && go list -m) || return 1
  files=$(printf '%s\n%s\n' "$changed" "$deleted" | grep -E "^$m/" | sed "s#^$m/##")
  (cd "$m" && go list -f '{{.ImportPath}} {{join .Imports " "}} {{join .TestImports " "}} {{join .XTestImports " "}}' ./... 2>/dev/null) | awk -v files="$files" -v mod="$mod" '
    function rel(p) { return p == mod ? "." : substr(p, length(mod) + 2) }
    { for (i = 2; i <= NF; i++) importers[$i] = importers[$i] " " $1; byDir[rel($1)] = $1 }
    END {
      n = split(files, list, "\n")
      for (k = 1; k <= n; k++) {
        d = list[k]
        if (d == "") continue
        while (1) {
          if (d ~ /\//) sub(/\/[^\/]*$/, "", d); else d = "."
          if (d in byDir) { queue[++tail] = byDir[d]; break }
          if (d == ".") break
        }
      }
      for (head = 1; head <= tail; head++) {
        p = queue[head]
        if (p in seen) continue
        seen[p] = 1
        print p
        count = split(importers[p], who, " ")
        for (j = 1; j <= count; j++) queue[++tail] = who[j]
      }
    }'
}

if touches '^services/go-api/'; then
  m=services/go-api
  go_pin $m
  if need go "go-api" && need golangci-lint "go-api lint"; then
    if [ $stage = commit ]; then
      mapfile -t staged_go < <(grep -E "^$m/.*\.go\$" <<<"$changed" | sed "s#^$m/##")
      if [ ${#staged_go[@]} -gt 0 ]; then
        mapfile -t staged_pkgs < <(printf '%s\n' "${staged_go[@]}" | xargs -r -d '\n' -n1 dirname | sort -u | sed 's#^#./#')
        check "go-api fast linters and format (pending packages)" $m env GOLANGCI_LINT_CACHE="$lint_cache" golangci-lint run --config .golangci.strict.yml --fast-only --new-from-rev=HEAD --allow-parallel-runners "${staged_pkgs[@]}"
        check "go-api no comments (pending files)" $m go run ./scripts/lintcomments --all "${staged_go[@]}"
        check "go-api no new vague names" $m go run ./scripts/lintnames "$base"
      fi
    else
      affected=()
      if touches "^$m/go\.(mod|sum)\$" || ! affected_out=$(go_affected $m); then
        whole_module=1
      else
        whole_module=0
        [ -n "$affected_out" ] && mapfile -t affected <<<"$affected_out"
      fi
      if [ $whole_module = 1 ]; then
        check "go-api vet" $m "${heavy[@]}" go vet ./...
      elif [ ${#affected[@]} -gt 0 ]; then
        check "go-api vet" $m "${heavy[@]}" go vet "${affected[@]}"
      fi
      check "go-api strict linters (new code)" $m "${heavy[@]}" env GOLANGCI_LINT_CACHE="$lint_cache" golangci-lint run --config .golangci.strict.yml --new-from-rev="$base" --allow-serial-runners
      check "go-api no comments (whole tree)" $m go run ./scripts/lintcomments --all .
      check "go-api no new vague names" $m go run ./scripts/lintnames "$base"
      mapfile -t pkgs < <(go_pkgs $m)
      [ ${#pkgs[@]} -gt 0 ] && test_check "go-api tests (race, changed packages)" $m "${heavy[@]}" go test -race -shuffle=on -count=1 "${pkgs[@]}"
      if [ ${#pkgs[@]} -gt 0 ] && [ "${INTEGRATION:-}" != 1 ]; then
        echo "SKIP  go-api mutation (changed lines): integration tests cover lines here, so it needs INTEGRATION=1 and a database; the backend-tests CI job runs it"
      elif [ ${#pkgs[@]} -gt 0 ] && need jq "go-api mutation"; then
        test_check "go-api mutation (changed lines)" . "${heavy[@]}" bash scripts/ci/go-mutate.sh "$base" $m
      fi
      if [ $whole_module = 1 ] || grep -qE '(^| )[^ ]*/cmd/acquisitioneval( |$)' <<<"${affected[*]}"; then
        test_check "go-api acquisition selection gate" $m "${heavy[@]}" go run ./cmd/acquisitioneval -baseline ./cmd/acquisitioneval/baselines.json
      fi
      need govulncheck "go-api govulncheck" && check "go-api govulncheck" $m "${heavy[@]}" govulncheck ./...
      check "go-api deadcode" $m "${heavy[@]}" bash "$root/scripts/ci/go-deadcode.sh"
    fi
  fi
fi

if touches '^services/go-api/migrations/|^scripts/migration-prefixes\.sh$'; then
  check "go-api migration numbers are unique" . bash scripts/migration-prefixes.sh services/go-api/migrations "$base_ref"
fi

if touches '\.sh$'; then
  check "no pipefail script pipes into grep -q" . bash scripts/pipefail-grep-q.sh
fi

if need go "secret scan" && need jq "secret scan"; then
  if [ $stage = commit ]; then
    check "secret scan (pending changes)" . bash "$root/scripts/ci/secret-scan.sh" --pending
  else
    check "secret scan (commits since base)" . bash "$root/scripts/ci/secret-scan.sh" "$base"
  fi
fi

script_tests=$(grep -E '^scripts/(ci/)?(__tests__/)?[a-z0-9-]+(\.test)?\.(sh|mjs)$' <<<"$changed" | grep -vE '/(precheck|test-home)(\.test)?\.(sh|mjs)$' | sed -E 's#^(scripts/(ci/)?)(__tests__/)?([a-z0-9-]+)(\.test)?\.(sh|mjs)$#\1__tests__/\4.test.sh#' | sort -u)
if touches '^client/(stryker\.config\.json|jest\.mutate\.config\.js)$'; then
  script_tests=$(printf '%s\n' $script_tests scripts/ci/__tests__/client-mutate.test.sh | sort -u)
fi
if [ -n "$script_tests" ] && [ $lint_only = 0 ] && ! link_deps .; then
  echo "SKIP  repo script self-tests: no root node_modules here or in $main_tree"; missing=1
elif [ -n "$script_tests" ]; then
  test_check "repo script self-tests" . bash -c 'for t in "$@"; do [ -f "$t" ] || continue; bash "$t" || exit 1; done' _ $script_tests
fi

if touches '^services/go-api/deploy/|^scripts/ci/test-backend-shellcheck\.sh$' && need shellcheck "go-api deploy shellcheck"; then
  check "go-api deploy shellcheck" services/go-api bash "$root/scripts/ci/test-backend-shellcheck.sh"
fi

if touches '^services/go-api/(deploy|migrations)/' && need docker "go-api deploy self-tests"; then
  test_check "go-api deploy self-tests" services/go-api bash -c 'for t in deploy/__tests__/*.test.sh; do bash "$t" || exit 1; done; bash deploy/migration-safety.sh'
fi

if touches '^scripts/uptime/'; then
  test_check "uptime probe self-tests" . sh -c 'bash scripts/uptime/__tests__/journey.test.sh && bash scripts/uptime/__tests__/check.test.sh'
fi

if touches '^services/supabase/'; then
  test_check "supabase auth config self-tests" . bash services/supabase/__tests__/supabase-auth.test.sh
fi

if touches '^(docs/features/[^/]+/notes\.md|services/go-api/internal/app/)' && need go "capability notes"; then
  go_pin services/go-api
  test_check "capability notes name mounted routes" services/go-api "${heavy[@]}" go test -count=1 ./internal/app -run TestCapabilityNotes_NameMountedRoutes
fi

if touches '^services/overseer/'; then
  m=services/overseer
  go_pin $m
  if need go "overseer" && need golangci-lint "overseer lint"; then
    if [ $stage = commit ]; then
      mapfile -t staged_overseer < <(grep -E "^$m/.*\.go\$" <<<"$changed" | sed "s#^$m/##")
      if [ ${#staged_overseer[@]} -gt 0 ]; then
        mapfile -t staged_overseer_pkgs < <(printf '%s\n' "${staged_overseer[@]}" | xargs -r -d '\n' -n1 dirname | sort -u | sed 's#^#./#')
        check "overseer fast linters and format (pending packages)" $m env GOLANGCI_LINT_CACHE="$lint_cache" golangci-lint run --config .golangci.strict.yml --fast-only --allow-parallel-runners "${staged_overseer_pkgs[@]}"
        check "overseer no comments (pending files)" services/go-api go run ./scripts/lintcomments --all "${staged_overseer[@]/#/../overseer/}"
      fi
    else
      check "overseer build" $m "${heavy[@]}" go build ./...
      check "overseer vet" $m "${heavy[@]}" go vet ./...
      check "overseer strict linters" $m "${heavy[@]}" env GOLANGCI_LINT_CACHE="$lint_cache" golangci-lint run --config .golangci.strict.yml --allow-serial-runners
      check "overseer no comments (whole tree)" services/go-api go run ./scripts/lintcomments --all ../overseer
      test_check "overseer tests (race)" $m "${heavy[@]}" env CGO_ENABLED=1 go test -race -shuffle=on -count=1 ./...
      touches '^services/overseer/go\.(mod|sum)$' && need govulncheck "overseer govulncheck" && check "overseer govulncheck" $m "${heavy[@]}" govulncheck ./...
    fi
  fi
  if touches '^services/overseer/web/' && need npm "overseer web"; then
    if link_deps $m/web; then
      check "overseer web typecheck" $m/web "${heavy[@]}" npm run --silent typecheck
      check "overseer web lint" $m/web "${heavy[@]}" npm run --silent lint
      test_check "overseer web test" $m/web "${heavy[@]}" npm run --silent test
      check "overseer web build and bundle budget" $m/web "${heavy[@]}" sh -c 'out=$(mktemp -d) && npx tsc --noEmit && npx vite build --outDir "$out" --emptyOutDir >/dev/null && node scripts/size.mjs "$out"; rc=$?; rm -rf "$out"; exit $rc'
    else
      echo "SKIP  overseer web: no node_modules here or in $main_tree"; missing=1
    fi
  fi
fi

if touches '^services/go-api/internal/(shared/events/|discovery/domain/events\.go)' && need npx "mobile contract tests"; then
  if link_deps client; then
    jest_check "mobile contract tests (go-api events changed)" client npx jest --ci --forceExit --watchman=false --cacheDirectory "$HOME/.cache/altune-ci/jest" '\.contract\.test\.tsx?$'
  else
    echo "SKIP  mobile contract tests: no node_modules here or in $main_tree"; missing=1
  fi
fi

if [ $lint_only = 0 ] && touches '^services/go-api/' && [ -d client/src ] && need npx "mobile contract tests"; then
  contract_tests=$(grep -rlF --include='*.test.ts' --include='*.test.tsx' "'go-api'" client/src | sed 's#^client/##' | sort)
  if [ -z "$contract_tests" ]; then
    [ $lint_only = 1 ] || { echo "FAIL  mobile contract tests (go-api changed): no client test file mentions 'go-api'"; record_fail "mobile contract tests (go-api changed)" 1 "no client test file mentions 'go-api'"; failed=1; }
  elif link_deps client; then
    jest_check "mobile contract tests (go-api changed)" client npx jest --ci --forceExit --watchman=false --testTimeout=30000 --cacheDirectory "$HOME/.cache/altune-ci/jest" $contract_tests
  else
    echo "SKIP  mobile contract tests: no node_modules here or in $main_tree"; missing=1
  fi
fi

if touches '^client/'; then
  m=client
  if need npx "mobile" && link_deps $m; then
    src=$(grep -E '^client/src/.*\.(ts|tsx)$' <<<"$changed" | sed 's#^client/##')
    check "mobile typecheck" $m "${heavy[@]}" npx tsc --noEmit
    linted=$(grep -E '^client/.*\.(ts|tsx|js|jsx|mjs|cjs)$' <<<"$changed" | sed 's#^client/##')
    [ -n "$linted" ] && check "mobile lint (changed files)" $m "${heavy[@]}" npx eslint $linted
    check "mobile mechanical style (changed lines)" $m node scripts/lint-changed-lines.mjs "$base"
    check "mobile prettier (changed files)" $m node scripts/prettier-changed.mjs "$base"
    test_check "mobile script and rule tests" $m bash -c 'files=$(git ls-files "scripts/__tests__/*.test.mjs" "eslint/rules/__tests__/*.test.js"); [ -z "$files" ] || node --test $files'
    touches '^client/eslint' && test_check "mobile eslint config tests" $m "${heavy[@]}" node --test eslint.config.test.js
    [ -n "$src" ] && jest_check "mobile tests (related)" $m npx jest --ci --passWithNoTests --forceExit --watchman=false --testTimeout=30000 --cacheDirectory "$HOME/.cache/altune-ci/jest" --findRelatedTests $src
    check "mobile fallow" $m npm run --silent fallow
    check "mobile boundaries and duplication (changed files)" $m "${heavy[@]}" node scripts/fallow-changed.mjs "$base"
    if [ -n "$src" ] && [ "${CLIENT_MUTATE_RUNS_ELSEWHERE:-}" = 1 ]; then
      echo "SKIP  mobile mutation (changed lines): the mobile-mutation CI job runs it"
    elif [ -n "$src" ] && need jq "mobile mutation"; then
      test_check "mobile mutation (changed lines)" . "${heavy[@]}" bash scripts/ci/client-mutate.sh "$base"
    fi
    check "mobile react-doctor" $m npm run --silent react-doctor
  elif [ -n "$(command -v npx)" ]; then
    echo "SKIP  mobile: no node_modules here or in $main_tree"; missing=1
  fi
fi

if touches '(_test\.go|\.(test|spec)\.[cm]?[jt]sx?)$|^scripts/test-home' && need node "test-home"; then
  current_step="test files live with their unit"
  node scripts/test-home.mjs "$base" >"$log" 2>&1
  case $? in
    0) echo "ok    test files live with their unit" ;;
    3) echo "SKIP  test-home: could not run"; tail -n 5 "$log" | sed 's/^/      /'; missing=1 ;;
    *) rc=$?; echo "FAIL  test files live with their unit"; tail -n 40 "$log" | sed 's/^/      /'; record_fail "test files live with their unit" "$rc" "$(cat "$log")"; failed=1 ;;
  esac
  if touches '^scripts/test-home'; then
    test_check "test-home regression suite" . bash scripts/test-home.test.sh
  fi
  if [ -f "$HOME/.claude/bin/vendor-test-home.sh" ]; then
    check "test-home in step with ~/.claude/bin" . bash "$HOME/.claude/bin/vendor-test-home.sh" "$root" --check
  fi
fi

if touches '^scripts/(__tests__/)?precheck(\.test)?\.sh$'; then
  test_check "precheck self-tests" . bash scripts/__tests__/precheck.test.sh
fi

workflow_files='^(\.github|\.gitea)/|^scripts/(gate-parity\.mjs|ci/pr-ci-dupes\.mjs|precheck\.sh)$'

if [ -f scripts/gate-parity.mjs ] && { [ $stage = push ] || touches "$workflow_files"; } && need node "gate parity" && link_deps .; then
  check "gate parity with main" . node scripts/gate-parity.mjs
fi

if [ -f scripts/ci/pr-ci-dupes.mjs ] && { [ $stage = push ] || touches "$workflow_files"; } && need node "duplicate PR CI work" && link_deps .; then
  check "no duplicate PR CI work" . node scripts/ci/pr-ci-dupes.mjs
fi

if [ -d services/go-api/scripts/nocomments ] && need go "nocomments"; then
  go_pin services/go-api
  if [ $stage = commit ]; then
    check "no comments (pending changes)" services/go-api go run ./scripts/nocomments diff HEAD
  else
    check "no comments (whole tree)" services/go-api go run ./scripts/nocomments check ../..
  fi
fi

finished=1
if [ $failed = 1 ]; then
  echo "precheck: red. Fix the FAIL lines, then rerun: bash scripts/precheck.sh $base_ref"
  printf '%s\n' "${fail_summaries[@]}"
  exit 1
fi
[ $missing = 1 ] && { echo "precheck: incomplete, see SKIP lines"; exit 3; }
[ $lint_only = 1 ] && { echo "precheck: lint green, tests left to CI"; exit 0; }
echo "precheck: green"
