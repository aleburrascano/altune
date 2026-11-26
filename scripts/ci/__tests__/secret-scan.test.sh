#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/secret-scan.sh
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

repo=$work/repo
git init -qb main "$repo"
g() { git -C "$repo" -c user.email=t@example.com -c user.name=t -c commit.gpgsign=false "$@"; }
printf 'one\n' >"$repo/a.txt"
g add -A
g commit -qm base
g switch -qc change
printf 'two\n' >"$repo/b.txt"
g add -A
g commit -qm change

mkdir "$work/bin"
cat >"$work/bin/go" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$STUB_LOG"
while [ $# -gt 0 ]; do
  [ "$1" = --report-path ] && { printf '%s' "${STUB_REPORT:-[]}" >"$2"; break; }
  shift
done
exit "${STUB_RC:-0}"
SH
chmod +x "$work/bin/go"
finding='[{"RuleID":"github-pat","File":"config.env","StartLine":3,"Commit":"0123456789abcdef0123","Secret":"REDACTED-BUT-DISTINCT-VALUE"}]'

run() { (cd "$repo" && PATH="$work/bin:$PATH" STUB_LOG="$work/go.log" "$@" 2>&1); }
t() {
  local want=$1 name=$2 contains=$3; shift 3
  local out rc=0
  out=$(run "$@") || rc=$?
  if [ "$rc" != "$want" ] || ! grep -qF -- "$contains" <<<"$out"; then
    echo "FAIL $name: exit $rc (want $want), output: $out"
    fail=1
  fi
  last=$out
}

: >"$work/go.log"
t 0 "a clean range passes" "no secrets added in main..HEAD" bash "$script" main
for want in "run github.com/zricethezav/gitleaks/v8@v8.30.1 git $repo" "--log-opts=main..HEAD" "--config $repo/.gitleaks.toml" "--redact=100"; do
  grep -qF -- "$want" "$work/go.log" || { echo "FAIL the gitleaks call lacks '$want': $(cat "$work/go.log")"; fail=1; }
done

t 1 "a finding fails and names rule, file, line and commit" "github-pat  config.env:3  commit 0123456789" env STUB_REPORT="$finding" bash "$script" main
grep -qF "REDACTED-BUT-DISTINCT-VALUE" <<<"$last" && { echo "FAIL a finding's secret value was printed"; fail=1; }

t 2 "a gitleaks run that fails is an error, not a pass" "nothing was checked" env STUB_RC=1 bash "$script" main
t 2 "an unknown base is an error, not a pass" "nothing was checked" bash "$script" no-such-ref
t 2 "no argument prints the usage" "usage:" bash "$script"

: >"$work/go.log"
t 0 "pending changes pass when clean" "no secrets added in the uncommitted changes" bash "$script" --pending
grep -qF -- "--pre-commit --staged" "$work/go.log" || { echo "FAIL --pending does not scan the index: $(cat "$work/go.log")"; fail=1; }
[ "$(grep -c -- "--pre-commit" "$work/go.log")" = 2 ] || { echo "FAIL --pending must run gitleaks on the index and on the working tree: $(cat "$work/go.log")"; fail=1; }

real=$work/real
git init -qb main "$real"
gr() { git -C "$real" -c user.email=t@example.com -c user.name=t -c commit.gpgsign=false "$@"; }
printf 'KEY=none\n' >"$real/config.env"
cp "$(dirname "$script")/../../.gitleaks.toml" "$real/.gitleaks.toml"
gr add -A
gr commit -qm base
token="ghp_$(LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 36)"
real_run() { (cd "$real" && bash "$script" "$@" 2>&1); }

printf 'KEY=%s\n' "$token" >"$real/config.env"
out=$(real_run --pending) && rc=0 || rc=$?
[ "$rc" = 1 ] || { echo "FAIL an unstaged secret was not caught by --pending (exit $rc): $out"; fail=1; }
gr add config.env
out=$(real_run --pending) && rc=0 || rc=$?
[ "$rc" = 1 ] || { echo "FAIL a staged secret was not caught by --pending (exit $rc): $out"; fail=1; }
grep -qF "$token" <<<"$out" && { echo "FAIL --pending printed the secret value"; fail=1; }
gr reset -q --hard
out=$(real_run --pending) && rc=0 || rc=$?
[ "$rc" = 0 ] || { echo "FAIL a clean tree failed --pending (exit $rc): $out"; fail=1; }

[ "$fail" = 0 ] && echo "secret-scan.test.sh: all cases pass"
exit "$fail"
