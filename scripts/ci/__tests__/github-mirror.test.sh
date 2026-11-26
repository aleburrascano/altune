#!/usr/bin/env bash
cd "$(dirname "$0")/../../.." || exit 1
root=$PWD
script=$root/scripts/ci/github-mirror.sh
workflow=$root/.gitea/workflows/github-mirror.yml
fail=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

cat >"$work/git" <<'STUB'
#!/usr/bin/env bash
echo "$* ssh=${GIT_SSH_COMMAND:-}" >>"$WORK/git.log"
case " $* " in
  *" rev-parse "*) echo "0123456789abcdef0123456789abcdef01234567" ;;
  *" push "*) exit "${PUSH_RC:-0}" ;;
esac
STUB
chmod +x "$work/git"

run() {
  : >"$work/git.log"
  PATH="$work:$PATH" WORK="$work" BRIDGE_GIT="$work/bridge.git" MIRROR_KEY=/k GITHUB_REMOTE=git@github.com:o/r.git \
    PUSH_RC=${1:-0} bash "$script" >"$work/out" 2>&1
  got=$?
}

check() {
  [ "$2" = "$3" ] || { echo "FAIL $1: want '$2' got '$3'"; fail=1; }
}

run 0
check "a good push exits 0" 0 "$got"
grep -qF -- "--git-dir $work/bridge.git push --force git@github.com:o/r.git refs/heads/main:refs/heads/main ssh=ssh -i /k -o IdentitiesOnly=yes" "$work/git.log" ||
  { echo "FAIL the push should come from the bridge repo with the mirror key: $(cat "$work/git.log")"; fail=1; }
grep -qF "github main is 0123456789ab" "$work/out" || { echo "FAIL the output should name the pushed commit: $(cat "$work/out")"; fail=1; }
grep -q -- " -C " "$work/git.log" && { echo "FAIL the mirror must not touch a working checkout"; fail=1; }

run 1
check "a failing push exits non-zero" 1 "$got"

PATH="$work:$PATH" WORK="$work" bash "$script" >"$work/out" 2>&1
check "a missing MIRROR_KEY exits non-zero" 1 "$([ $? = 0 ] && echo 0 || echo 1)"

if grep -qE 'projects/altune|git -C' "$workflow"; then
  echo "FAIL the mirror workflow must not push from the main checkout (its pre-push hook queues on the heavy slots)"
  fail=1
fi
grep -qF 'scripts/ci/github-mirror.sh' "$workflow" || { echo "FAIL the workflow should run the repo script"; fail=1; }

[ "$fail" = 0 ] && echo "github-mirror tests ok"
[ "$fail" = 0 ]
