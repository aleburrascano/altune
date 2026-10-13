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
touches() { grep -qE "$1" <<<"$changed"; }

main_tree=$(git worktree list --porcelain | awk '/^worktree /{print $2; exit}')
link_deps() {
  local out
  out=$(bash scripts/worktree-deps.sh "$1") || return 1
  eval "$out"
}

heavy=()
[ -f "$HOME/.claude/bin/heavy.sh" ] && heavy=(bash "$HOME/.claude/bin/heavy.sh")

hash_comments() {
  git diff -U0 --diff-filter=ACMR "$base" -- "$@" | awk '
    /^\+\+\+ b\// { file = substr($0, 7); next }
    /^@@/ { split($3, a, ","); line = substr(a[1], 2) - 1; next }
    /^\+/ { line++; if ($0 ~ /^\+[[:space:]]*#([[:space:]]|$)/ || $0 ~ /^\+.*[[:space:]]#([[:space:]]|$)/) { print file ":" line ": " substr($0, 2); hits++ } }
    END { exit hits > 0 }'
}

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
  grep -E "^$m/.*\.go$" <<<"$changed" | xargs -r -n1 dirname | sort -u \
    | sed "s#^$m#.#" | while read -r d; do
      [ -d "$m/$d" ] || continue
      (cd "$m" && go list "$d") >/dev/null 2>&1 && echo "$d"
    done
}

if touches '^services/go-api/'; then
  m=services/go-api
  go_pin $m
  if need go "go-api" && need golangci-lint "go-api lint"; then
    check "go-api vet" $m "${heavy[@]}" go vet ./...
    check "go-api import direction" $m "${heavy[@]}" golangci-lint run --allow-serial-runners
    check "go-api strict linters (new code)" $m "${heavy[@]}" golangci-lint run --config .golangci.strict.yml --new-from-rev="$base" --allow-serial-runners
    check "go-api no new comments" $m go run ./scripts/lintcomments "$base"
    check "go-api no comments (whole tree)" $m go run ./scripts/lintcomments --all .
    check "go-api no new vague names" $m go run ./scripts/lintnames "$base"
    check "go-api module shape" $m go run ./scripts/moduleshape
    mapfile -t pkgs < <(go_pkgs $m)
    [ ${#pkgs[@]} -gt 0 ] && test_check "go-api tests (changed packages)" $m "${heavy[@]}" go test -shuffle=on -count=1 "${pkgs[@]}"
    test_check "go-api acquisition selection gate" $m "${heavy[@]}" go run ./cmd/acquisitioneval -baseline ./cmd/acquisitioneval/baselines.json
    need govulncheck "go-api govulncheck" && check "go-api govulncheck" $m "${heavy[@]}" govulncheck ./...
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
    check "overseer no new comments" services/go-api go run ./scripts/lintcomments "$base" ../overseer
    check "overseer no comments (whole tree)" services/go-api go run ./scripts/lintcomments --all ../overseer
    test_check "overseer tests (race)" $m "${heavy[@]}" env CGO_ENABLED=1 go test -race -shuffle=on -count=1 ./...
    need govulncheck "overseer govulncheck" && check "overseer govulncheck" $m "${heavy[@]}" govulncheck ./...
    need nilaway "overseer nilaway ceiling" && check "overseer nilaway ceiling" $m "${heavy[@]}" bash "$root/scripts/ci/nilaway-ceiling.sh"
  fi
  if touches '^services/overseer/web/' && need npm "overseer web"; then
    if link_deps $m/web; then
      check "overseer web typecheck" $m/web "${heavy[@]}" npm run --silent typecheck
      check "overseer web lint" $m/web "${heavy[@]}" npm run --silent lint
      check "overseer web no new comments" $m/web node scripts/lint-changed-comments.mjs "$base"
      test_check "overseer web test" $m/web "${heavy[@]}" npm run --silent test
      check "overseer web build and bundle budget" $m/web "${heavy[@]}" sh -c 'out=$(mktemp -d) && npx tsc --noEmit && npx vite build --outDir "$out" --emptyOutDir >/dev/null && node scripts/size.mjs "$out"; rc=$?; rm -rf "$out"; exit $rc'
    else
      echo "SKIP  overseer web: no node_modules here or in $main_tree"; missing=1
    fi
  fi
fi

if touches '^(services/go-api|services/overseer)/.*\.go$'; then
  m=services/go-api
  go_pin $m
  if need go "stripcomments"; then
    test_check "stripcomments self-tests" $m "${heavy[@]}" go test -count=1 ./scripts/stripcomments/...
    strip_dir=$(mktemp -d)
    strip_bin="$strip_dir/stripcomments"
    if (cd $m && go build -o "$strip_bin" ./scripts/stripcomments) >"$log" 2>&1; then
      mismatches=""
      while IFS= read -r f; do
        [ -f "$f" ] || continue
        work=$(mktemp -d)
        if git show "$base:$f" >"$work/base.go" 2>/dev/null; then
          cp "$f" "$work/pr.go"
          cp "$work/pr.go" "$work/pr.orig"
          if "$strip_bin" "$work/pr.go" >/dev/null 2>&1 && cmp -s "$work/pr.orig" "$work/pr.go"; then
            cp "$work/base.go" "$work/base.orig"
            if "$strip_bin" "$work/base.go" >/dev/null 2>&1 && ! cmp -s "$work/base.orig" "$work/base.go" && ! cmp -s "$work/base.go" "$work/pr.orig"; then
              base_nows=$(tr -d '[:space:]' <"$work/base.go")
              pr_nows=$(tr -d '[:space:]' <"$work/pr.orig")
              [ "$base_nows" = "$pr_nows" ] && mismatches="$mismatches $f"
            fi
          fi
        fi
        rm -rf "$work"
      done < <(grep -E '^(services/go-api|services/overseer)/.*\.go$' <<<"$changed")
      rm -rf "$strip_dir"
      if [ -n "$mismatches" ]; then
        echo "FAIL  strip PRs match the strip tool   ($mismatches)"
        failed=1
      else
        echo "ok    strip PRs match the strip tool"
      fi
    else
      echo "FAIL  strip PRs match the strip tool   (build stripcomments)"
      tail -n 40 "$log" | sed 's/^/      /'
      rm -rf "$strip_dir"
      failed=1
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
    [ -n "$src" ] && check "mobile lint (changed files)" $m "${heavy[@]}" npx eslint $src
    check "mobile mechanical style (changed lines)" $m node scripts/lint-changed-lines.mjs "$base"
    check "mobile prettier (changed files)" $m node scripts/prettier-changed.mjs "$base"
    test_check "mobile script and rule tests" $m bash -c 'files=$(git ls-files "scripts/__tests__/*.test.mjs" "eslint-rules/__tests__/*.test.js"); [ -z "$files" ] || node --test $files'
    coverage_args=(); [ -n "$src" ] && mapfile -t coverage_args < <(thresholded_coverage_args "$src")
    [ -n "$src" ] && jest_check "mobile tests (related)" $m npx jest "${coverage_args[@]}" --ci --passWithNoTests --forceExit --watchman=false --testTimeout=30000 --cacheDirectory "$HOME/.cache/altune-ci/jest" --findRelatedTests $src
    check "mobile consistency ratchet" $m node scripts/consistency-ratchet.mjs
    check "mobile own-module mock ratchet" $m node scripts/mock-ratchet.mjs
    check "mobile fallow" $m npm run --silent fallow
    check "mobile react-doctor" $m "${heavy[@]}" bash "$root/scripts/ci/test-mobile-react-doctor.sh" "$base"
    check "mobile react-doctor (changed lines)" $m node scripts/react-doctor-changed-lines.mjs "$base" doctor-report.json
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

if touches '\.(go|ts|tsx)$' && need npx "cycles"; then
  if link_deps .; then
    check "no dependency cycles" . "${heavy[@]}" sh -c 'node_modules/.bin/graft build >/dev/null && node scripts/check-cycles.mjs'
  else
    echo "SKIP  cycles: no root node_modules"; missing=1
  fi
fi

if touches '^(scripts/[^/]*|[^/]+)\.[cm]?[jt]sx?$' && need node "repo scripts comments"; then
  if link_deps services/overseer/web; then
    check "repo scripts no new comments" . node services/overseer/web/scripts/lint-changed-comments.mjs "$base" \
      ':(top,glob)*.js' ':(top,glob)*.jsx' ':(top,glob)*.ts' ':(top,glob)*.tsx' ':(top,glob)*.mjs' ':(top,glob)*.cjs' \
      ':(top,glob)scripts/*.js' ':(top,glob)scripts/*.jsx' ':(top,glob)scripts/*.ts' ':(top,glob)scripts/*.tsx' ':(top,glob)scripts/*.mjs' ':(top,glob)scripts/*.cjs'
  else
    echo "SKIP  repo scripts comments: no services/overseer/web/node_modules here or in $main_tree"; missing=1
  fi
fi

if touches '^\.(github|gitea)/workflows/.*\.ya?ml$|\.sh$'; then
  check "workflows and shell no new comments" . hash_comments '.github/workflows/*.yml' '.github/workflows/*.yaml' '.gitea/workflows/*.yml' '.gitea/workflows/*.yaml' '*.sh'
fi

if [ -d services/go-api/scripts/nocomments ] && need go "nocomments"; then
  go_pin services/go-api
  check "no comments (whole tree)" services/go-api go run ./scripts/nocomments check ../..
fi

[ $failed = 1 ] && { echo "precheck: red. Fix the FAIL lines, then rerun: bash scripts/precheck.sh $base_ref"; exit 1; }
[ $missing = 1 ] && { echo "precheck: incomplete, see SKIP lines"; exit 3; }
[ $lint_only = 1 ] && { echo "precheck: lint green, tests left to CI"; exit 0; }
echo "precheck: green"
