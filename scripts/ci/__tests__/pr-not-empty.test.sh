#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/pr-not-empty.sh
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

repo=$work/repo
git init -qb main "$repo"
g() { git -C "$repo" -c user.email=t@example.com -c user.name=t -c commit.gpgsign=false "$@"; }
printf 'one\n' >"$repo/a.txt"
g add -A
g commit -qm base

run() { (cd "$repo" && bash "$script" "$@" 2>&1); }
t() {
  local want=$1 name=$2 contains=$3; shift 3
  local out rc=0
  out=$(run "$@") || rc=$?
  if [ "$rc" != "$want" ] || ! grep -qF -- "$contains" <<<"$out"; then
    echo "FAIL $name: exit $rc (want $want), output: $out"
    fail=1
  fi
}

g switch -qc empty
t 1 "a branch equal to main is empty" "has no changes beyond main" main

g switch -qc reverted
printf 'two\n' >"$repo/b.txt"
g add -A; g commit -qm add
g rm -q b.txt; g commit -qm revert
t 1 "a branch whose commits cancel out is empty" "has no changes beyond main" main

g switch -q main
g switch -qc changed
printf 'two\n' >"$repo/b.txt"
g add -A; g commit -qm add
t 0 "a branch with a change passes" "has changes beyond main" main

g switch -q main
printf 'three\n' >"$repo/c.txt"
g add -A; g commit -qm "main moves on"
g switch -q empty
t 1 "a branch with no changes of its own is empty even when main moved on" "has no changes beyond main" main
g switch -q changed
t 0 "a branch with a change passes when main moved on" "has changes beyond main" main

t 2 "an unknown base is an error, not a pass" "nothing was checked" no-such-ref
t 2 "no argument prints the usage" "usage:"

mkdir "$work/fakebin"
cat >"$work/fakebin/git" <<'SH'
#!/usr/bin/env bash
[ "$1" = diff ] && { echo "fatal: simulated failure" >&2; exit 128; }
exec "$REAL_GIT" "$@"
SH
chmod +x "$work/fakebin/git"
out=$(cd "$repo" && PATH="$work/fakebin:$PATH" REAL_GIT=$(command -v git) bash "$script" main 2>&1) && rc=0 || rc=$?
if [ "$rc" != 2 ] || ! grep -qF "nothing was checked" <<<"$out"; then
  echo "FAIL a failing git diff is an error, not a pass: exit $rc, output: $out"
  fail=1
fi

[ "$fail" = 0 ] && echo "pr-not-empty.test.sh: all cases pass"
exit "$fail"
