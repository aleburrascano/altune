#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/merge-result.sh
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT
mkdir "$work/scratch"

repo=$work/repo
g() { git -C "$repo" -c user.email=t@example.com -c user.name=t "$@"; }
commit_file() { mkdir -p "$(dirname "$repo/$1")"; printf '%s\n' "$2" >"$repo/$1"; g add -A; g commit -qm "$3"; }

git init -qb main "$repo"
mkdir "$repo/scripts"
printf '#!/usr/bin/env bash\nexit 0\n' >"$repo/scripts/check.sh"
printf 'one\ntwo\nthree\n' >"$repo/shared.txt"
g add -A; g commit -qm base
g tag fork

g checkout -qb pr
commit_file new.txt "uses the legacy call" "the pull request adds legacy code"
g checkout -q main
printf '#!/usr/bin/env bash\n! grep -rq legacy --exclude-dir=.git --exclude=check.sh .\n' >"$repo/scripts/check.sh"
g add -A; g commit -qm "main now rejects legacy code"

run() { TMPDIR=$work/scratch bash "$script" "$repo" "$@" >"$work/out" 2>"$work/err"; }
runs_check() {
  local wt
  wt=$(mktemp -d "$work/wt.XXXXXX"); rmdir "$wt"
  g worktree add -q --detach "$wt" "$1"
  (cd "$wt" && bash scripts/check.sh)
  local rc=$?
  g worktree remove --force "$wt"
  return $rc
}

if ! runs_check pr; then
  echo "FAIL stale head: the old scripts were expected to pass the stale head"; fail=1
fi
if run main pr; then
  merged=$(cat "$work/out")
  if runs_check "$merged"; then
    echo "FAIL stale head: the merge result passed although main's new rule rejects it"; fail=1
  fi
  [ "$(g show "$merged:scripts/check.sh" | md5sum)" = "$(g show main:scripts/check.sh | md5sum)" ] || { echo "FAIL stale head: the merge result does not carry main's script"; fail=1; }
  [ "$(g rev-parse "$merged^1")" = "$(g rev-parse main)" ] && [ "$(g rev-parse "$merged^2")" = "$(g rev-parse pr)" ] || { echo "FAIL stale head: the result is not main with the pull request merged in"; fail=1; }
  run main pr && [ "$(cat "$work/out")" = "$merged" ] || { echo "FAIL the same two commits gave a different result commit"; fail=1; }
else
  echo "FAIL stale head: exited non-zero"; sed 's/^/    /' "$work/err"; fail=1
fi
[ -z "$(ls -A "$work/scratch")" ] || { echo "FAIL left behind $(ls -A "$work/scratch")"; fail=1; }
[ "$(g worktree list | wc -l)" = 1 ] || { echo "FAIL worktree still registered"; fail=1; }

g checkout -qb weakens main
printf '#!/usr/bin/env bash\nexit 0 # weakened by the pull request\n' >"$repo/scripts/check.sh"
commit_file scripts/extra.sh "echo extra" "the pull request changes a script"
g checkout -q main
commit_file unrelated.txt "y" "main moves on"
if run main weakens; then
  merged=$(cat "$work/out")
  [ "$(g show "$merged:scripts/extra.sh")" = "echo extra" ] || { echo "FAIL a script the pull request adds is missing from the merge result"; fail=1; }
  [ "$(g show "$merged:scripts/check.sh" | md5sum)" = "$(g show weakens:scripts/check.sh | md5sum)" ] || { echo "FAIL a script the pull request changes is not the one in the merge result"; fail=1; }
else
  echo "FAIL pull request that changes a script: exited non-zero"; sed 's/^/    /' "$work/err"; fail=1
fi

g checkout -qb up-to-date main
commit_file up.txt "x" "on top of main"
g checkout -q main
if run main up-to-date; then
  [ "$(cat "$work/out")" = "$(g rev-parse up-to-date)" ] || { echo "FAIL a head already on main should be returned as it is"; fail=1; }
else
  echo "FAIL head already on main: exited non-zero"; fail=1
fi

g checkout -qb clash fork
commit_file shared.txt "$(printf 'one\nTWO from the pull request\nthree')" "the pull request edits the shared line"
g checkout -q main
commit_file shared.txt "$(printf 'one\nTWO from main\nthree')" "main edits the shared line"
if run main clash; then
  echo "FAIL conflict: exited zero"; fail=1
fi
[ ! -s "$work/out" ] || { echo "FAIL conflict: printed a commit anyway: $(cat "$work/out")"; fail=1; }
grep -q "update your branch onto main" "$work/err" || { echo "FAIL conflict: does not say to update the branch onto main"; sed 's/^/    /' "$work/err"; fail=1; }
grep -q "shared.txt" "$work/err" || { echo "FAIL conflict: does not name the file"; fail=1; }
[ "$(TMPDIR=$work/scratch bash "$script" "$repo" main clash >/dev/null 2>&1; echo $?)" = 1 ] || { echo "FAIL conflict: exit status is not 1"; fail=1; }
[ -z "$(ls -A "$work/scratch")" ] || { echo "FAIL conflict: left behind $(ls -A "$work/scratch")"; fail=1; }
[ "$(g worktree list | wc -l)" = 1 ] || { echo "FAIL conflict: worktree still registered"; fail=1; }

[ "$(TMPDIR=$work/scratch bash "$script" "$repo" main no-such-ref >/dev/null 2>&1; echo $?)" = 2 ] || { echo "FAIL missing ref: exit status is not 2"; fail=1; }
[ "$(bash "$script" 2>/dev/null; echo $?)" = 2 ] || { echo "FAIL no arguments: exit status is not 2"; fail=1; }

[ "$fail" = 0 ] && echo "merge-result: all passed"
exit "$fail"
