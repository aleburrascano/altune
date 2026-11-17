#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/step-worktree.sh
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

repo=$work/runner/altune
git init -qb main "$repo"
mkdir -p "$repo/scripts/ci" "$work/scratch"
cat >"$repo/scripts/worktree-deps.sh" <<'SH'
echo "export DEPS_FOR='$*'"
[ -z "${DEPS_FAIL:-}" ] || { echo "worktree-deps: npm ci failed" >&2; exit 1; }
SH
git -C "$repo" add -A
git -C "$repo" -c user.email=t@example.com -c user.name=t commit -qm "before the setup script"
git -C "$repo" tag old
cp "$script" "$repo/scripts/ci/"
git -C "$repo" add -A
git -C "$repo" -c user.email=t@example.com -c user.name=t commit -qm base
printf '#!/usr/bin/env bash\necho "$1" >"%s"\n' "$work/refreshed" >"$work/runner/refresh-deps.sh"

step() {
  local envs=()
  while [ "$1" != -- ]; do envs+=("$1"); shift; done
  shift
  env ALTUNE_REPO="$repo" TMPDIR="$work/scratch" "${envs[@]}" bash -c '
    set -euo pipefail
    setup=$(git -C "$ALTUNE_REPO" show "$2:scripts/ci/step-worktree.sh")
    . /dev/stdin "$@" <<<"$setup"
    echo "in=$PWD deps=$DEPS_FOR"
  ' _ "$@" >"$work/out" 2>&1
}

if step -- origin-main main . client; then
  grep -q "^in=$work/scratch/ci-wf\.[^ ]* deps=. client$" "$work/out" || { echo "FAIL good ref: wrong place or deps"; sed 's/^/    /' "$work/out"; fail=1; }
else
  echo "FAIL good ref: exited non-zero"; sed 's/^/    /' "$work/out"; fail=1
fi
[ -z "$(ls -A "$work/scratch")" ] || { echo "FAIL good ref: left behind $(ls -A "$work/scratch")"; fail=1; }
[ "$(git -C "$repo" worktree list | wc -l)" = 1 ] || { echo "FAIL good ref: worktree still registered"; fail=1; }
[ ! -e "$work/refreshed" ] || { echo "FAIL refreshed deps on a runner that does not own them"; fail=1; }

step ALTUNE_CI_OWNED=1 -- gitea/main main || { echo "FAIL CI-owned runner: exited non-zero"; sed 's/^/    /' "$work/out"; fail=1; }
[ "$(cat "$work/refreshed" 2>/dev/null)" = gitea/main ] || { echo "FAIL CI-owned runner: refresh-deps not run for gitea/main"; fail=1; }

if ALTUNE_REPO=$repo TMPDIR=$work/scratch bash -c 'set -euo pipefail; . "$1" main no-such-ref; echo "ran on"' _ "$script" >"$work/out" 2>&1; then
  echo "FAIL missing ref: exited zero"; fail=1
fi
grep -q "ran on" "$work/out" && { echo "FAIL missing ref: the step carried on"; fail=1; }
[ -z "$(ls -A "$work/scratch")" ] || { echo "FAIL missing ref: left behind $(ls -A "$work/scratch")"; fail=1; }

if step DEPS_FAIL=1 -- main main . client; then echo "FAIL deps that do not install: exited zero"; fail=1; fi
grep -q "^in=" "$work/out" && { echo "FAIL deps that do not install: the step carried on"; fail=1; }
grep -q "npm ci failed" "$work/out" || { echo "FAIL deps that do not install: does not say why"; fail=1; }
[ -z "$(ls -A "$work/scratch")" ] || { echo "FAIL deps that do not install: left behind $(ls -A "$work/scratch")"; fail=1; }

if step -- main old; then echo "FAIL ref with no setup file: exited zero"; fail=1; fi
grep -q "^in=" "$work/out" && { echo "FAIL ref with no setup file: the step carried on"; fail=1; }

if env -u ALTUNE_REPO bash -c '. "$1" main main' _ "$script" >"$work/out" 2>&1; then
  echo "FAIL no ALTUNE_REPO: exited zero"; fail=1
fi
grep -q "ALTUNE_REPO" "$work/out" || { echo "FAIL no ALTUNE_REPO: does not say so"; fail=1; }

[ "$fail" = 0 ] && echo "step-worktree: all passed"
exit "$fail"
