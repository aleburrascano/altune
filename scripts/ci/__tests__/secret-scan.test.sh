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

[ "$fail" = 0 ] && echo "secret-scan.test.sh: all cases pass"
exit "$fail"
