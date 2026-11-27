#!/usr/bin/env bash
set -uo pipefail

stage=push
full=0
while [ $# -gt 0 ]; do
  case "$1" in
    --lint) stage=commit; shift ;;
    --full) full=1; shift ;;
    -*) echo "usage: precheck.sh [--lint] [--full] [BASE_REF]" >&2; exit 2 ;;
    *) break ;;
  esac
done
[ $# -le 1 ] || { echo "usage: precheck.sh [--lint] [--full] [BASE_REF]" >&2; exit 2; }
lint_only=0
[ $stage = commit ] && lint_only=1

root=$(git rev-parse --show-toplevel) || exit 3
cd "$root" || exit 3
export GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=diff.renameLimit GIT_CONFIG_VALUE_0=20000
export GOFLAGS=-mod=readonly
lint_cache="$root/.cache/golangci-lint"
default_base_ref() { git remote get-url gitea >/dev/null 2>&1 && git rev-parse --verify --quiet gitea/main >/dev/null && echo gitea/main || echo origin/main; }
base_ref=${1:-$(default_base_ref)}
base=$(git merge-base "$base_ref" HEAD) || { echo "precheck: no merge base with $base_ref. Fetch it with: git fetch ${base_ref%%/*} ${base_ref#*/}"; exit 3; }

PATH="$PATH:$(go env GOPATH 2>/dev/null)/bin"

result_dir="$root/.cache/precheck"
result_key=
if [ $stage = push ] && [ "${PRECHECK_NO_CACHE:-}" != 1 ] && [ -z "$(git status --porcelain)" ]; then
  result_key=$(
    {
      git rev-parse 'HEAD^{tree}'
      echo "$base"
      git rev-parse "$base_ref"
      go version 2>/dev/null
      golangci-lint --version 2>/dev/null
      node --version 2>/dev/null
      jq --version 2>/dev/null
      shellcheck --version 2>/dev/null
      date -u +%Y-%m-%d
      echo "integration=${INTEGRATION:-} client_mutate_elsewhere=${CLIENT_MUTATE_RUNS_ELSEWHERE:-} full=$full"
    } | sha256sum | cut -d' ' -f1
  )
  if [ -f "$result_dir/$result_key" ]; then
    echo "precheck: green, already proven for this exact tree, base and tool versions ($(cat "$result_dir/$result_key")); rerun with PRECHECK_NO_CACHE=1 to force"
    exit 0
  fi
fi

untracked() { [ $stage = commit ] || git ls-files --others --exclude-standard; }
if [ $stage = commit ]; then
  base=$(git rev-parse HEAD) || { echo "precheck: no HEAD commit to compare the pending changes with"; exit 3; }
  if merging=$(git rev-parse -q --verify MERGE_HEAD) && merge_base=$(git merge-base "$base_ref" "$merging"); then
    base=$merge_base
  fi
  changed=$(git -c core.quotePath=false diff --name-only --diff-filter=ACMR "$base" | sort -u)
  deleted=$(git -c core.quotePath=false diff --name-only --no-renames --diff-filter=D "$base")
else
  changed=$( { git diff --name-only --diff-filter=ACMR "$base"; untracked; } | sort -u)
  deleted=$(git diff --name-only --no-renames --diff-filter=D "$base")
fi
touches() { grep -qE "$1" <<<"$changed"$'\n'"$deleted"; }

main_tree=$(git worktree list --porcelain | awk '/^worktree /{print $2; exit}')
declare -A deps_env=() deps_rc=()
link_deps() {
  if [ -z "${deps_rc[$1]:-}" ]; then
    deps_rc[$1]=0
    deps_env[$1]=$(bash scripts/worktree-deps.sh "$1") || deps_rc[$1]=1
  fi
  [ "${deps_rc[$1]}" = 0 ] || return 1
  eval "${deps_env[$1]}"
}

heavy=()
[ -f "$HOME/.claude/bin/heavy.sh" ] && heavy=(bash "$HOME/.claude/bin/heavy.sh")
db_env=()
[ ${#heavy[@]} -gt 0 ] && [ -z "${HEAVY_HELD:-}" ] && [ -z "${DATABASE_URL:-}${TEST_DATABASE_URL:-}" ] && db_env=(env INTEGRATION=1)

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
par_dir=
par_ids=()
par_pids=()

par() {
  if [ -n "${HEAVY_HELD:-}" ]; then
    "$@"
    return
  fi
  local id=${#par_ids[@]}
  [ -n "$par_dir" ] || par_dir=$(mktemp -d)
  par_ids+=("$id")
  printf '%s' "$2" >"$par_dir/$id.name"
  (
    log=$(mktemp)
    trap 'rm -f "$log"' EXIT
    failed=0
    fail_summaries=()
    "$@"
    [ ${#fail_summaries[@]} -eq 0 ] || printf '%s\0' "${fail_summaries[@]}" >"$par_dir/$id.summaries"
    printf '%s' "$failed" >"$par_dir/$id.failed"
  ) >"$par_dir/$id.out" 2>&1 &
  par_pids+=($!)
}

par_wait() {
  local i id summary
  for i in "${!par_ids[@]}"; do
    id=${par_ids[$i]}
    current_step="waiting for the steps running side by side"
    wait "${par_pids[$i]}" || true
    cat "$par_dir/$id.out"
    : >"$par_dir/$id.shown"
    if [ ! -f "$par_dir/$id.failed" ]; then
      failed=1
      echo "FAIL  $(cat "$par_dir/$id.name") ended before it finished"
      record_fail "$(cat "$par_dir/$id.name")" 1 "it ended before it finished"
      continue
    fi
    [ "$(cat "$par_dir/$id.failed")" = 1 ] && failed=1
    if [ -f "$par_dir/$id.summaries" ]; then
      while IFS= read -r -d '' summary; do fail_summaries+=("$summary"); done <"$par_dir/$id.summaries"
    fi
  done
  par_ids=()
  par_pids=()
}

stop_tree() {
  local child
  for child in $(ps -o pid= --ppid "$1" 2>/dev/null); do stop_tree "$child"; done
  kill "$1" 2>/dev/null || true
}

par_stop() {
  local i id running=()
  for i in "${!par_ids[@]}"; do
    id=${par_ids[$i]}
    if [ -f "$par_dir/$id.failed" ]; then
      [ -f "$par_dir/$id.shown" ] || cat "$par_dir/$id.out"
    elif [ -f "$par_dir/$id.shown" ]; then
      :
    else
      running+=("$(cat "$par_dir/$id.name")")
      ! kill -0 "${par_pids[$i]}" 2>/dev/null || stop_tree "${par_pids[$i]}"
    fi
  done
  [ ${#running[@]} -eq 0 ] || printf 'still running when stopped: %s\n' "${running[@]}"
  [ -z "$par_dir" ] || rm -rf "$par_dir"
}

trap 'exit_rc=$?; par_stop; abort_rc=${abort_rc:-$exit_rc}; announce_abort; rm -f "$log"' EXIT
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

go_test_failures() {
  local failures others
  failures=$(awk '
    race { print; if ($0 == "==================") race = 0; next }
    pan && pan_lines < 40 { print; pan_lines++; next }
    /^WARNING: DATA RACE/ { race = 1; print; next }
    /^panic:/ { pan = 1; pan_lines = 1; print; next }
    /^--- FAIL:/ { on = 1; print; next }
    /^FAIL/ { on = 0; print; next }
    on && /^[[:space:]]/ { print; next }
    { on = 0 }
  ' "$log")
  if grep -qE '^(--- FAIL:|panic:|WARNING: DATA RACE)' <<<"$failures"; then
    head -n 300 <<<"$failures"
    [ "$(wc -l <<<"$failures")" -le 300 ] || echo "(cut at 300 lines)"
    echo "(only the failed tests and their output are shown; rerun the command above for the logs of passing tests)"
    return
  fi
  others=$(grep -vE '^(ok|\?)[[:space:]]' "$log")
  if [ -z "$others" ]; then
    log_tail
  else
    head -n 200 <<<"$others"
    [ "$(wc -l <<<"$others")" -le 200 ] || echo "(cut at 200 lines; rerun the command above for the rest)"
  fi
}

go_test_check() {
  [ $lint_only = 1 ] || run_check go_test_failures "$@"
}

jest_limit=${PRECHECK_JEST_TIMEOUT:-1800}
jest_cache=../.cache/jest

jest_failed_suites() {
  sed -E 's/\x1b\[[0-9;]*m//g' "$log" | awk -v limit=200 '
    function emit(text) {
      if (shown >= limit) { cut = 1; return }
      print text
      shown++
    }
    /^[[:space:]]*FAIL[[:space:]]/ {
      path = $2
      for (i = 2; i <= NF; i++) if ($i ~ /\.(test|spec)\.[cm]?[jt]sx?$/) { path = $i; break }
      capturing = !(path in seen)
      seen[path] = 1
      count = 0
      inconsole = 0
      if (capturing) emit("  " path)
      next
    }
    /^[[:space:]]*(PASS[[:space:]]|Summary of all failing tests)/ { capturing = 0; next }
    /^(Test Suites:|Tests:|Snapshots?:|Snapshot Summary|Time:|Ran all test suites)/ { capturing = 0; next }
    /^[[:space:]]*● Console$/ { inconsole = 1; next }
    /^[[:space:]]*● / { inconsole = 0 }
    capturing && !inconsole && NF > 0 && !/^[[:space:]]+at / && !/^[[:space:]]*>?[[:space:]]*[0-9]*[[:space:]]*\|/ && count < 6 {
      sub(/^[[:space:]]+/, "")
      emit("      " $0)
      count++
    }
    END { if (cut) print "  (list cut at " limit " lines; more suites failed than are shown)" }'
}

jest_failures() {
  if [ "$last_rc" = 124 ] || [ "$last_rc" = 137 ]; then
    echo "jest did not exit within ${jest_limit}s and was killed (raise PRECHECK_JEST_TIMEOUT if the suite is just slow)"
  fi
  if grep -qF 'JavaScript heap out of memory' "$log"; then
    echo "a jest worker ran out of memory (the V8 heap limit, about 2 GB on chudbox). Jest reports that kill as 'terminated by another process: signal=SIGTERM'; nothing outside jest sent it."
    echo "client/jest.config.js caps this with workerIdleMemoryLimit: rebase onto main if your branch lacks it. If it has it, one suite allocates more than the cap between checks: run the suites named below alone."
  fi
  local names suites
  names=$(sed -E 's/\x1b\[[0-9;]*m//g' "$log" | grep -E '^[[:space:]]*● ' | grep -vE '● Console$' | sed -E 's/^[[:space:]]+//' | awk '!seen[$0]++')
  suites=$(jest_failed_suites)
  if [ -n "$names" ]; then
    echo "failed tests:"
    echo "$names"
    sed -E 's/\x1b\[[0-9;]*m//g' "$log" | grep -E '^(Test Suites|Tests):'
    if [ -n "$suites" ]; then
      echo "failed suites (path, then the first lines of its error):"
      echo "$suites"
    fi
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
    if [ $stage = commit ] || [ $full = 0 ]; then
      mapfile -t staged_go < <(grep -E "^$m/.*\.go\$" <<<"$changed" | sed "s#^$m/##")
      if [ ${#staged_go[@]} -gt 0 ]; then
        mapfile -t staged_pkgs < <(printf '%s\n' "${staged_go[@]}" | xargs -r -d '\n' -n1 dirname | sort -u | sed 's#^#./#')
        scoped_rev=$base
        check "go-api fast linters and format (changed packages)" $m env GOLANGCI_LINT_CACHE="$lint_cache" golangci-lint run --config .golangci.strict.yml --fast-only --new-from-rev="$scoped_rev" --allow-parallel-runners "${staged_pkgs[@]}"
        check "go-api no comments (changed files)" $m go run ./scripts/lintcomments --all "${staged_go[@]}"
        check "go-api no new vague names" $m go run ./scripts/lintnames "$base"
      fi
      if [ $stage = push ]; then
        if touches "^$m/.*\.go\$|^$m/\.go-arch-lint\.yml\$" && need go-arch-lint "go-api architecture"; then
          check "go-api architecture: every module imports only what its role may (go-arch-lint)" $m go-arch-lint check
        fi
        if touches "^$m/.*\.go\$"; then
          check "go-api module doors: test support imported only from tests, no unused root export (lintdoors --exports)" $m go run ./scripts/lintdoors --exports .
        fi
        if touches "^$m/go\.(mod|sum)\$"; then
          check "go-api go.mod and go.sum are tidy" $m go mod tidy -diff
        fi
        if touches "^$m/.*\.go\$|^$m/go\.(mod|sum)\$"; then
          par check "go-api vet (whole module, so an importer a change broke fails here)" $m "${heavy[@]}" go vet ./...
        fi
        if [ ${#staged_go[@]} -gt 0 ]; then
          par check "go-api strict linters (changed packages, every linter, as CI runs them)" $m "${heavy[@]}" env GOLANGCI_LINT_CACHE="$lint_cache" golangci-lint run --config .golangci.strict.yml --new-from-rev="$base" --allow-parallel-runners "${staged_pkgs[@]}"
        fi
        mapfile -t pkgs < <(go_pkgs $m)
        [ ${#pkgs[@]} -gt 0 ] && par go_test_check "go-api tests (race, changed packages)" $m "${heavy[@]}" go test -race -shuffle=on -count=1 "${pkgs[@]}"
      fi
    else
      affected=()
      if [ "${PRECHECK_WHOLE_MODULE:-}" = 1 ] || touches "^$m/(go\.(mod|sum)|\.golangci[^/]*|_rules/.*|tools\.go)\$" || ! affected_out=$(go_affected $m); then
        whole_module=1
      else
        whole_module=0
        [ -n "$affected_out" ] && mapfile -t affected <<<"$affected_out"
      fi
      if touches "^$m/go\.(mod|sum)\$"; then
        check "go-api go.mod and go.sum are tidy" $m go mod tidy -diff
      fi
      if [ $whole_module = 1 ]; then
        par check "go-api vet" $m "${heavy[@]}" go vet ./...
      elif [ ${#affected[@]} -gt 0 ]; then
        par check "go-api vet" $m "${heavy[@]}" go vet "${affected[@]}"
      fi
      if [ $whole_module = 1 ]; then
        par check "go-api strict linters (whole module)" $m "${heavy[@]}" env GOLANGCI_LINT_CACHE="$lint_cache" golangci-lint run --config .golangci.strict.yml --allow-serial-runners ./...
      elif [ ${#affected[@]} -gt 0 ]; then
        par check "go-api strict linters (changed packages and their importers)" $m "${heavy[@]}" env GOLANGCI_LINT_CACHE="$lint_cache" golangci-lint run --config .golangci.strict.yml --allow-serial-runners "${affected[@]}"
      fi
      check "go-api no comments (whole tree)" $m go run ./scripts/lintcomments --all .
      check "history is whole, so the whole-tree checks see every file" . bash scripts/history-complete.sh
      check "go-api no vague names (whole tree)" $m go run ./scripts/lintnames "$(git rev-list --max-parents=0 HEAD | tail -n 1)"
      need go-arch-lint "go-api architecture" && check "go-api architecture: every module imports only what its role may (go-arch-lint, whole tree)" $m go-arch-lint check
      check "go-api module doors: test support imported only from tests, no unused root export (lintdoors --exports, whole tree)" $m go run ./scripts/lintdoors --exports .
      mapfile -t pkgs < <(go_pkgs $m)
      if [ $whole_module = 1 ]; then test_pkgs=(./...); else test_pkgs=("${affected[@]}"); fi
      [ ${#pkgs[@]} -gt 0 ] && [ ${#test_pkgs[@]} -gt 0 ] && par go_test_check "go-api tests (race, changed packages and their importers)" $m "${heavy[@]}" "${db_env[@]}" go test -race -shuffle=on -count=1 "${test_pkgs[@]}"
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
if touches '^scripts/(__tests__/(precheck|acq-debug|temp-cleanup)\.test\.sh|ci/deploy-web-smoke\.sh)$'; then
  script_tests=$(printf '%s\n' $script_tests scripts/__tests__/temp-cleanup.test.sh | sort -u)
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

if touches '^(docs/features/[^/]+/notes\.md|services/go-api/internal/app/)' && need go "capability notes" && ! { [ $lint_only = 0 ] && [[ " ${pkgs[*]:-} " == *" ./internal/app "* ]]; }; then
  go_pin services/go-api
  par go_test_check "capability notes name mounted routes" services/go-api "${heavy[@]}" go test -count=1 ./internal/app -run TestCapabilityNotes_NameMountedRoutes
fi

if touches '^services/overseer/'; then
  m=services/overseer
  go_pin $m
  if need go "overseer" && need golangci-lint "overseer lint"; then
    if [ $stage = commit ] || [ $full = 0 ]; then
      mapfile -t staged_overseer < <(grep -E "^$m/.*\.go\$" <<<"$changed" | sed "s#^$m/##")
      if [ ${#staged_overseer[@]} -gt 0 ]; then
        mapfile -t staged_overseer_pkgs < <(printf '%s\n' "${staged_overseer[@]}" | xargs -r -d '\n' -n1 dirname | sort -u | sed 's#^#./#')
        check "overseer fast linters and format (changed packages)" $m env GOLANGCI_LINT_CACHE="$lint_cache" golangci-lint run --config .golangci.strict.yml --fast-only --allow-parallel-runners "${staged_overseer_pkgs[@]}"
        check "overseer no comments (changed files)" services/go-api go run ./scripts/lintcomments --all "${staged_overseer[@]/#/../overseer/}"
      fi
      if [ $stage = push ]; then
        go_test_check "overseer tests (race)" $m "${heavy[@]}" env CGO_ENABLED=1 go test -race -shuffle=on -count=1 ./...
      fi
    else
      check "overseer build" $m "${heavy[@]}" go build ./...
      check "overseer vet" $m "${heavy[@]}" go vet ./...
      check "overseer strict linters" $m "${heavy[@]}" env GOLANGCI_LINT_CACHE="$lint_cache" golangci-lint run --config .golangci.strict.yml --allow-serial-runners
      check "overseer no comments (whole tree)" services/go-api go run ./scripts/lintcomments --all ../overseer
      go_test_check "overseer tests (race)" $m "${heavy[@]}" env CGO_ENABLED=1 go test -race -shuffle=on -count=1 ./...
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

if [ $lint_only = 0 ] && touches '^services/go-api/internal/(shared/events/|search/internal/domain/events\.go)' && need npx "mobile contract tests"; then
  if link_deps client; then
    par jest_check "mobile contract tests (go-api events changed)" client npx jest --ci --forceExit --watchman=false --cacheDirectory "$jest_cache" '\.contract\.test\.tsx?$'
  else
    echo "SKIP  mobile contract tests: no node_modules here or in $main_tree"; missing=1
  fi
fi

if [ $lint_only = 0 ] && touches '^services/go-api/' && [ -d client/src ] && need npx "mobile contract tests"; then
  contract_tests=$(grep -rlF --include='*.test.ts' --include='*.test.tsx' "'go-api'" client/src | sed 's#^client/##' | sort)
  if [ -z "$contract_tests" ]; then
    echo "FAIL  mobile contract tests (go-api changed): no client test file mentions 'go-api'"; record_fail "mobile contract tests (go-api changed)" 1 "no client test file mentions 'go-api'"; failed=1
  elif link_deps client; then
    par jest_check "mobile contract tests (go-api changed)" client npx jest --ci --forceExit --watchman=false --testTimeout=30000 --cacheDirectory "$jest_cache" $contract_tests
  else
    echo "SKIP  mobile contract tests: no node_modules here or in $main_tree"; missing=1
  fi
fi

if touches '^client/'; then
  m=client
  if need npx "mobile" && link_deps $m; then
    src=$(grep -E '^client/src/.*\.(ts|tsx)$' <<<"$changed" | sed 's#^client/##')
    linted=$(grep -E '^client/.*\.(ts|tsx|js|jsx|mjs|cjs)$' <<<"$changed" | sed 's#^client/##')
    if [ $stage = commit ]; then
      [ -n "$linted" ] && check "mobile lint (pending files, no type info)" $m env ESLINT_WITHOUT_TYPES=1 npx eslint $linted
      check "mobile mechanical style (pending lines, no type info)" $m env ESLINT_WITHOUT_TYPES=1 node scripts/lint-changed-lines.mjs "$base"
    else
      check "mobile typecheck" $m "${heavy[@]}" npx tsc --noEmit
      [ -n "$linted" ] && check "mobile lint (changed files)" $m "${heavy[@]}" npx eslint $linted
      check "mobile mechanical style (changed lines)" $m node scripts/lint-changed-lines.mjs "$base"
    fi
    check "mobile prettier (changed files)" $m node scripts/prettier-changed.mjs "$base"
    test_check "mobile script and rule tests" $m bash -c 'files=$(git ls-files "scripts/__tests__/*.test.mjs" "eslint/rules/__tests__/*.test.js"); [ -z "$files" ] || node --test $files'
    touches '^client/eslint' && test_check "mobile eslint config tests" $m "${heavy[@]}" node --test eslint.config.test.js
    touches '^client/jest\.config' && test_check "mobile jest config tests" $m "${heavy[@]}" node --test jest.config.test.js
    jest_check "mobile doctor config test (reads the whole client tree)" $m npx jest --ci --forceExit --watchman=false --testTimeout=30000 --cacheDirectory "$jest_cache" __tests__/doctor.config.test.ts
    [ -n "$src" ] && jest_check "mobile tests (related)" $m npx jest --ci --passWithNoTests --forceExit --watchman=false --testTimeout=30000 --cacheDirectory "$jest_cache" --findRelatedTests $src
    test_check "mobile fallow" $m npm run --silent fallow
    test_check "mobile boundaries and duplication (changed files)" $m "${heavy[@]}" node scripts/fallow-changed.mjs "$base"
    if [ -n "$src" ] && [ "${CLIENT_MUTATE_RUNS_ELSEWHERE:-}" = 1 ]; then
      echo "SKIP  mobile mutation (changed lines): the mobile-mutation CI job runs it"
    elif [ -n "$src" ] && need jq "mobile mutation"; then
      test_check "mobile mutation (changed lines)" . "${heavy[@]}" bash scripts/ci/client-mutate.sh "$base"
    fi
    test_check "mobile react-doctor" $m npm run --silent react-doctor
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

if [ "$full" = 1 ] && [ $stage != commit ] && need node "test-home whole tree"; then
  check "go-api and overseer test files live with their unit (whole tree)" . bash scripts/test-home-tree.sh services/go-api services/overseer
fi

if touches '^scripts/(__tests__/)?precheck(\.test)?\.sh$'; then
  test_check "precheck self-tests" . bash scripts/__tests__/precheck.test.sh
fi

workflow_files='^(\.github|\.gitea)/|^scripts/(ci/|gate-parity\.mjs|precheck\.sh$)'

if [ -f scripts/gate-parity.mjs ] && { [ $stage = push ] || touches "$workflow_files"; } && need node "gate parity" && link_deps .; then
  check "gate parity with main" . node scripts/gate-parity.mjs
fi

if [ -f scripts/ci/pr-ci-dupes.mjs ] && { [ $stage = push ] || touches "$workflow_files"; } && need node "duplicate PR CI work" && link_deps .; then
  check "no duplicate PR CI work" . node scripts/ci/pr-ci-dupes.mjs
fi

if [ -d services/go-api/scripts/nocomments ] && need go "nocomments"; then
  go_pin services/go-api
  if [ $stage = commit ]; then
    check "no comments (pending changes)" services/go-api go run ./scripts/nocomments diff "$base"
  else
    check "no comments (whole tree)" services/go-api go run ./scripts/nocomments check ../..
  fi
fi

par_wait
finished=1
if [ $failed = 1 ]; then
  echo "precheck: red. Fix the FAIL lines, then rerun: bash scripts/precheck.sh $([ $full = 1 ] && echo '--full ')$base_ref"
  printf '%s\n' "${fail_summaries[@]}"
  exit 1
fi
[ $missing = 1 ] && { echo "precheck: incomplete, see SKIP lines"; exit 3; }
[ $lint_only = 1 ] && { echo "precheck: lint green, tests left to CI"; exit 0; }
if [ -n "$result_key" ]; then
  { mkdir -p "$result_dir" && date -u +"green at %Y-%m-%dT%H:%M:%SZ" >"$result_dir/$result_key"; } \
    || echo "precheck: could not record this green run in $result_dir, so the next push will rerun it"
fi
echo "precheck: green"
