#!/usr/bin/env bash
set -uo pipefail

lint_only=0
[ "${1:-}" = --lint ] && { lint_only=1; shift; }

root=$(git rev-parse --show-toplevel) || exit 3
cd "$root" || exit 3
default_base_ref() { git rev-parse --verify --quiet gitea/main >/dev/null && echo gitea/main || echo origin/main; }
base_ref=${1:-$(default_base_ref)}
base=$(git merge-base "$base_ref" HEAD) || { echo "precheck: no merge base with $base_ref"; exit 3; }

PATH="$PATH:$(go env GOPATH 2>/dev/null)/bin"

untracked() { [ $lint_only = 1 ] || git ls-files --others --exclude-standard; }
changed=$( { git diff --name-only --diff-filter=ACMR "$base"; untracked; } | sort -u)
deleted=$(git diff --name-only --no-renames --diff-filter=D "$base")
touches() { grep -qE "$1" <<<"$changed"$'\n'"$deleted"; }

import_line='^[+-][[:space:]]*(import[[:space:](]|([A-Za-z_.][A-Za-z0-9_]*[[:space:]]+)?"[^"]+"[[:space:]]*$|export[[:space:]].*[[:space:]]from[[:space:]]|\}[[:space:]]*from[[:space:]]+[^[:space:]]|from[[:space:]]+[^[:space:]])|require\(|import\('
changes_imports() {
  local sources='\.(go|ts|tsx)$'
  [ -n "$(git diff --name-status --no-renames --diff-filter=AD "$base" | grep -E "$sources")" ] && return 0
  [ -n "$(untracked | grep -E "$sources")" ] && return 0
  [ -n "$(git diff -U0 --no-color "$base" -- '*.go' '*.ts' '*.tsx' | grep -E "$import_line")" ]
}

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
trap 'rm -f "$log"' EXIT

last_rc=0
run_check() {
  local show=$1 name=$2 dir=$3; shift 3
  (cd "$dir" && "$@") >"$log" 2>&1
  last_rc=$?
  if [ $last_rc = 0 ]; then
    echo "ok    $name"
  else
    echo "FAIL  $name   ($dir: $*)"
    "$show" | sed 's/^/      /'
    failed=1
  fi
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
  printf '%s\n%s\n' "$changed" "$deleted" | grep -E "^$m/.*\.go$" | xargs -r -n1 dirname | sort -u \
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
    import_scope=(--new-from-rev="$base")
    if touches '(^|/)\.golangci[^/]*\.yml$' || [ -n "$(git diff -M --name-only --diff-filter=RC "$base")" ] || [ -n "$(untracked | grep -E '\.go$')" ]; then
      import_scope=()
    fi
    check "go-api import direction" $m "${heavy[@]}" golangci-lint run "${import_scope[@]}" --allow-serial-runners
    check "go-api strict linters (new code)" $m "${heavy[@]}" golangci-lint run --config .golangci.strict.yml --new-from-rev="$base" --allow-serial-runners
    check "go-api no comments (whole tree)" $m go run ./scripts/lintcomments --all .
    check "go-api no new vague names" $m go run ./scripts/lintnames "$base"
    check "go-api module shape" $m go run ./scripts/moduleshape
    mapfile -t pkgs < <(go_pkgs $m)
    [ ${#pkgs[@]} -gt 0 ] && test_check "go-api tests (changed packages)" $m "${heavy[@]}" go test -shuffle=on -count=1 "${pkgs[@]}"
    if [ $whole_module = 1 ] || grep -qE '(^| )[^ ]*/cmd/acquisitioneval( |$)' <<<"${affected[*]}"; then
      test_check "go-api acquisition selection gate" $m "${heavy[@]}" go run ./cmd/acquisitioneval -baseline ./cmd/acquisitioneval/baselines.json
    fi
    touches "^$m/go\.(mod|sum)\$" && need govulncheck "go-api govulncheck" && check "go-api govulncheck" $m "${heavy[@]}" govulncheck ./...
  fi
fi

if touches '^services/go-api/migrations/|^scripts/migration-prefixes\.sh$'; then
  check "go-api migration numbers are unique" . bash scripts/migration-prefixes.sh services/go-api/migrations "$base_ref"
fi

if touches '\.sh$'; then
  check "no pipefail script pipes into grep -q" . bash scripts/pipefail-grep-q.sh
fi

script_tests=$(grep -E '^scripts/(ci/)?(__tests__/)?[a-z0-9-]+(\.test)?\.(sh|mjs)$' <<<"$changed" | grep -vE '/(precheck|test-home)(\.test)?\.(sh|mjs)$' | sed -E 's#^(scripts/(ci/)?)(__tests__/)?([a-z0-9-]+)(\.test)?\.(sh|mjs)$#\1__tests__/\4.test.sh#' | sort -u)
if [ -n "$script_tests" ] && [ $lint_only = 0 ] && ! link_deps .; then
  echo "SKIP  repo script self-tests: no root node_modules here or in $main_tree"; missing=1
elif [ -n "$script_tests" ]; then
  test_check "repo script self-tests" . bash -c 'for t in "$@"; do [ -f "$t" ] || continue; bash "$t" || exit 1; done' _ $script_tests
fi

if touches '^services/go-api/deploy/|^scripts/ci/test-backend-shellcheck\.sh$' && need shellcheck "go-api deploy shellcheck"; then
  check "go-api deploy shellcheck" services/go-api bash "$root/scripts/ci/test-backend-shellcheck.sh"
fi

if touches '^(services/go-api/(deploy|migrations)/|client/src/shared/killSwitch/killSwitchPoll\.ts$)' && need docker "go-api deploy self-tests"; then
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
    check "overseer build" $m "${heavy[@]}" go build ./...
    check "overseer vet" $m "${heavy[@]}" go vet ./...
    check "overseer strict linters" $m "${heavy[@]}" golangci-lint run --config "$root/services/go-api/.golangci.strict.yml" --disable=funlen,revive --allow-serial-runners
    check "overseer no comments (whole tree)" services/go-api go run ./scripts/lintcomments --all ../overseer
    test_check "overseer tests (race)" $m "${heavy[@]}" env CGO_ENABLED=1 go test -race -shuffle=on -count=1 ./...
    touches '^services/overseer/go\.(mod|sum)$' && need govulncheck "overseer govulncheck" && check "overseer govulncheck" $m "${heavy[@]}" govulncheck ./...
    need nilaway "overseer nilaway ceiling" && check "overseer nilaway ceiling" $m "${heavy[@]}" bash "$root/scripts/ci/nilaway-ceiling.sh"
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

if touches '^services/go-api/internal/(shared/events/|discovery/domain/events\.go)' && need npx "mobile event contracts"; then
  if link_deps client; then
    jest_check "mobile event contracts (go-api events changed)" client npx jest --ci --forceExit --watchman=false --cacheDirectory "$HOME/.cache/altune-ci/jest" src/shared/events/__tests__/eventContract.test.ts src/shared/telemetry/__tests__/eventContract.test.ts
  else
    echo "SKIP  mobile event contracts: no node_modules here or in $main_tree"; missing=1
  fi
fi

if touches '^services/go-api/' && [ -d client/src ] && need npx "mobile contract tests"; then
  contract_tests=$(grep -rlF --include='*.test.ts' --include='*.test.tsx' "'go-api'" client/src | sed 's#^client/##' | sort)
  if [ -z "$contract_tests" ]; then
    [ $lint_only = 1 ] || { echo "FAIL  mobile contract tests (go-api changed): no client test file mentions 'go-api'"; failed=1; }
  elif link_deps client; then
    jest_check "mobile contract tests (go-api changed)" client npx jest --ci --forceExit --watchman=false --testTimeout=30000 --cacheDirectory "$HOME/.cache/altune-ci/jest" $contract_tests
  else
    echo "SKIP  mobile contract tests: no node_modules here or in $main_tree"; missing=1
  fi
fi

thresholded_coverage_args() {
  (cd client && node -e '
    const thresholds = require("./jest.config.js").coverageThreshold;
    const files = process.argv.slice(1).filter((f) => !/\/__tests__\/|\.d\.ts$/.test(f));
    const covers = (glob, file) => file.startsWith(glob.replace(/\*\*$/, ""));
    const enforced = Object.fromEntries(
      Object.entries(thresholds).filter(([glob]) => glob !== "global" && files.some((f) => covers(glob, f))),
    );
    const checked = files.filter((f) => Object.keys(enforced).some((glob) => covers(glob, f)));
    if (checked.length === 0) process.exit(0);
    console.log(["--coverage", "--coverageThreshold=" + JSON.stringify(enforced), ...checked.map((f) => "--collectCoverageFrom=" + f)].join("\n"));
  ' $1)
}

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
    coverage_args=(); [ -n "$src" ] && mapfile -t coverage_args < <(thresholded_coverage_args "$src")
    [ -n "$src" ] && jest_check "mobile tests (related)" $m npx jest "${coverage_args[@]}" --ci --passWithNoTests --forceExit --watchman=false --testTimeout=30000 --cacheDirectory "$HOME/.cache/altune-ci/jest" --findRelatedTests $src
    check "mobile consistency ratchet" $m node scripts/consistency-ratchet.mjs
    check "mobile own-module mock ratchet" $m node scripts/mock-ratchet.mjs
    check "mobile fallow" $m npm run --silent fallow
    check "mobile complexity ratchet (fallow)" $m bash "$root/scripts/ci/test-mobile-complexity.sh"
    check "mobile react-doctor (changed lines)" $m node scripts/react-doctor-changed-lines.mjs "$base"
    check "mobile module shape" $m node scripts/module-shape.mjs
  elif [ -n "$(command -v npx)" ]; then
    echo "SKIP  mobile: no node_modules here or in $main_tree"; missing=1
  fi
fi

if touches '(_test\.go|\.(test|spec)\.[cm]?[jt]sx?)$|^scripts/test-home' && need node "test-home"; then
  node scripts/test-home.mjs "$base" >"$log" 2>&1
  case $? in
    0) echo "ok    test files live with their unit" ;;
    3) echo "SKIP  test-home: could not run"; tail -n 5 "$log" | sed 's/^/      /'; missing=1 ;;
    *) echo "FAIL  test files live with their unit"; tail -n 40 "$log" | sed 's/^/      /'; failed=1 ;;
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

if [ -f scripts/gate-parity.mjs ] && need node "gate parity" && link_deps .; then
  check "gate parity with main" . node scripts/gate-parity.mjs
fi

if [ -f scripts/ci/pr-ci-dupes.mjs ] && need node "duplicate PR CI work" && link_deps .; then
  check "no duplicate PR CI work" . node scripts/ci/pr-ci-dupes.mjs
fi

if changes_imports && need npx "cycles"; then
  if link_deps .; then
    check "no dependency cycles" . "${heavy[@]}" sh -c 'node_modules/.bin/graft build >/dev/null && node scripts/check-cycles.mjs'
  else
    echo "SKIP  cycles: no root node_modules"; missing=1
  fi
fi

if [ -d services/go-api/scripts/nocomments ] && need go "nocomments"; then
  go_pin services/go-api
  check "no comments (whole tree)" services/go-api go run ./scripts/nocomments check ../..
fi

[ $failed = 1 ] && { echo "precheck: red. Fix the FAIL lines, then rerun: bash scripts/precheck.sh $base_ref"; exit 1; }
[ $missing = 1 ] && { echo "precheck: incomplete, see SKIP lines"; exit 3; }
[ $lint_only = 1 ] && { echo "precheck: lint green, tests left to CI"; exit 0; }
echo "precheck: green"
