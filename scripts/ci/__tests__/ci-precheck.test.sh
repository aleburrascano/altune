#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/ci-precheck.sh
fail=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

repo=$work/repo
mkdir -p "$repo/scripts"
git init -qb main "$repo"
g() { git -C "$repo" -c user.email=t@example.com -c user.name=t -c commit.gpgsign=false "$@"; }
cat >"$repo/scripts/precheck.sh" <<STUB
#!/usr/bin/env bash
printf 'args=%s lint_whole=%s mutate_elsewhere=%s\n' "\$*" "\${PRECHECK_LINT_WHOLE:-}" "\${CLIENT_MUTATE_RUNS_ELSEWHERE:-}" >"$work/call.log"
exit "\${STUB_RC:-0}"
STUB
printf 'one\n' >"$repo/a.txt"
g add -A
g commit -qm one
first=$(g rev-parse HEAD)
printf 'two\n' >"$repo/a.txt"
g add -A
g commit -qm two
second=$(g rev-parse HEAD)
printf 'three\n' >"$repo/a.txt"
g add -A
g commit -qm three
g update-ref refs/remotes/origin/main "$first"

run() { (cd "$repo" && env "$@" bash "$script" 2>&1); }
t() {
  local want=$1 name=$2 contains=$3; shift 3
  local out rc=0
  : >"$work/call.log"
  out=$(run "$@") || rc=$?
  if [ "$rc" != "$want" ] || ! grep -qF -- "$contains" "$work/call.log"; then
    echo "FAIL $name: exit $rc (want $want), call: $(cat "$work/call.log"), output: $out"
    fail=1
  fi
}

t 0 "a pull request is checked against origin/main with the whole module linted" "args=origin/main lint_whole=1 mutate_elsewhere=1" GITHUB_REF=refs/pull/1/merge
t 0 "a push to main is checked against the commit before the push" "args=$first lint_whole=1" GITHUB_REF=refs/heads/main BEFORE="$first"
t 0 "a push to main with an all-zero before falls back to the parent commit" "args=$second lint_whole=1" GITHUB_REF=refs/heads/main BEFORE=0000000000000000000000000000000000000000
t 0 "a push to main with an unknown before falls back to the parent commit" "args=$second lint_whole=1" GITHUB_REF=refs/heads/main BEFORE=1111111111111111111111111111111111111111
t 1 "a red precheck is a red run" "args=origin/main" GITHUB_REF=refs/pull/1/merge STUB_RC=1

rm -rf "$repo/.git"
git init -qb main "$repo"
g add -A
g commit -qm only
out=$(run GITHUB_REF=refs/heads/main BEFORE=0000000000000000000000000000000000000000) && rc=0 || rc=$?
if [ "$rc" != 2 ] || ! grep -qF "nothing was checked" <<<"$out"; then
  echo "FAIL a push to main with no parent must be an error, not a pass: exit $rc, output: $out"
  fail=1
fi

out=$(cd "$repo" && bash "$script" extra 2>&1) && rc=0 || rc=$?
if [ "$rc" != 2 ] || ! grep -qF "usage:" <<<"$out"; then
  echo "FAIL an argument must print the usage and exit 2: exit $rc, output: $out"
  fail=1
fi

[ "$fail" = 0 ] && echo "ci-precheck.test.sh: all cases pass"
exit "$fail"
