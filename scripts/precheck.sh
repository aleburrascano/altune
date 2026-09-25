#!/usr/bin/env bash
# Local parity with the PR gate, scoped to what this branch changed, so a PR
# goes up green instead of bouncing on a rule CI would have caught. Runs the
# fast, blocking checks of test-backend, test-overseer, test-mobile and the
# cycles and test-home jobs for each side the diff touches. Left to CI:
# govulncheck, nilaway, integration-tagged tests (need Postgres), the coverage
# and fallow ratchets, react-doctor.
#
# Usage: bash scripts/precheck.sh [base-ref]   (default origin/main)
# Exit: 0 green, 1 a check failed, 3 could not run (a toolchain is missing).
set -uo pipefail

root=$(git rev-parse --show-toplevel) || exit 3
cd "$root" || exit 3
base=$(git merge-base "${1:-origin/main}" HEAD) || { echo "precheck: no merge base with ${1:-origin/main}"; exit 3; }

nvm_node22=$(ls -d "$HOME"/.nvm/versions/node/v22.*/bin 2>/dev/null | sort -V | tail -1)
[ -n "$nvm_node22" ] && PATH="$nvm_node22:$PATH"
PATH="$PATH:$(go env GOPATH 2>/dev/null)/bin"

changed=$( { git diff --name-only --diff-filter=ACMR "$base"; git ls-files --others --exclude-standard; } | sort -u)
touches() { grep -qE "$1" <<<"$changed"; }

main_tree=$(git worktree list --porcelain | awk '/^worktree /{print $2; exit}')
link_deps() { # <dir>: a fresh worktree has no node_modules; borrow the main checkout's
  [ -d "$1/node_modules" ] && return 0
  [ -d "$main_tree/$1/node_modules" ] || return 1
  ln -s "$main_tree/$1/node_modules" "$1/node_modules"
  local exclude
  exclude="$(git rev-parse --git-common-dir)/info/exclude"
  grep -qx 'node_modules' "$exclude" 2>/dev/null || echo 'node_modules' >>"$exclude"
}

failed=0
missing=0
log=$(mktemp)
trap 'rm -f "$log"' EXIT

check() { # <name> <dir> <command...>
  local name=$1 dir=$2; shift 2
  if (cd "$dir" && "$@") >"$log" 2>&1; then
    echo "ok    $name"
  else
    echo "FAIL  $name   ($dir: $*)"
    tail -n 40 "$log" | sed 's/^/      /'
    failed=1
  fi
}

need() { command -v "$1" >/dev/null 2>&1 || { echo "SKIP  $2: $1 not installed"; missing=1; return 1; }; }

go_pin() { # <module dir>: golangci-lint v2.12.2 panics on the Go 1.27 stdlib, so pin each module's go.mod version, as CI does
  export GOTOOLCHAIN="go$(awk '/^go /{print $2; exit}' "$1/go.mod")"
}

go_pkgs() { # <module dir>: the packages holding changed .go files, relative to it
  grep -E "^$1/.*\.go$" <<<"$changed" | xargs -r -n1 dirname | sort -u \
    | sed "s#^$1#.#" | while read -r d; do [ -d "$1/$d" ] && echo "$d"; done
}

if touches '^services/go-api/'; then
  m=services/go-api
  go_pin $m
  if need go "go-api" && need golangci-lint "go-api lint"; then
    check "go-api vet" $m go vet -tags integration ./...
    check "go-api import direction" $m golangci-lint run --build-tags integration
    check "go-api strict linters (new code)" $m golangci-lint run --config .golangci.strict.yml --build-tags integration --new-from-rev="$base"
    check "go-api no new comments" $m go run scripts/lint-changed-comments.go "$base"
    check "go-api no new vague names" $m go run scripts/lint-changed-names.go "$base"
    mapfile -t pkgs < <(go_pkgs $m)
    [ ${#pkgs[@]} -gt 0 ] && check "go-api tests (changed packages)" $m go test -count=1 "${pkgs[@]}"
  fi
fi

if touches '^services/overseer/'; then
  m=services/overseer
  go_pin $m
  if need go "overseer" && need golangci-lint "overseer lint"; then
    check "overseer build" $m go build ./...
    check "overseer vet" $m go vet ./...
    check "overseer strict linters" $m golangci-lint run --config "$root/services/go-api/.golangci.strict.yml" --disable=funlen,revive
    mapfile -t pkgs < <(go_pkgs $m)
    [ ${#pkgs[@]} -gt 0 ] && check "overseer tests (changed packages)" $m go test -count=1 "${pkgs[@]}"
  fi
  if touches '^services/overseer/web/' && need npm "overseer web"; then
    if link_deps $m/web; then
      check "overseer web typecheck" $m/web npm run --silent typecheck
      check "overseer web lint" $m/web npm run --silent lint
      check "overseer web test" $m/web npm run --silent test
    else
      echo "SKIP  overseer web: no node_modules here or in $main_tree"; missing=1
    fi
  fi
fi

if touches '^apps/mobile/'; then
  m=apps/mobile
  if need npx "mobile" && link_deps $m; then
    src=$(grep -E '^apps/mobile/src/.*\.(ts|tsx)$' <<<"$changed" | sed 's#^apps/mobile/##')
    check "mobile typecheck" $m npx tsc --noEmit
    [ -n "$src" ] && check "mobile lint (changed files)" $m npx eslint $src
    check "mobile mechanical style (changed lines)" $m node scripts/lint-changed-lines.mjs "$base"
    [ -n "$src" ] && check "mobile tests (related)" $m npx jest --ci --passWithNoTests --findRelatedTests $src
  elif [ -n "$(command -v npx)" ]; then
    echo "SKIP  mobile: no node_modules here or in $main_tree"; missing=1
  fi
fi

if touches '(_test\.go|\.(test|spec)\.[cm]?[jt]sx?)$' && need node "test-home"; then
  check "test files live with their unit" . node scripts/test-home.mjs "$base"
fi

if touches '\.(go|ts|tsx)$' && need npx "cycles"; then
  if link_deps .; then
    check "no dependency cycles" . sh -c 'node_modules/.bin/graft build >/dev/null && node scripts/check-cycles.mjs'
  else
    echo "SKIP  cycles: no root node_modules"; missing=1
  fi
fi

[ $failed = 1 ] && { echo "precheck: red. Fix the FAIL lines, then rerun: bash scripts/precheck.sh"; exit 1; }
[ $missing = 1 ] && { echo "precheck: incomplete, see SKIP lines"; exit 3; }
echo "precheck: green"
