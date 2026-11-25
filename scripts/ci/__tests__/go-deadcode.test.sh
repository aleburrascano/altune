#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/go-deadcode.sh
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

mkdir -p "$work/scripts/ci" "$work/services/go-api/internal/deep/er" "$work/bin" "$work/elsewhere"
cp "$script" "$work/scripts/ci/"
printf 'module altune/go-api\n' >"$work/services/go-api/go.mod"
module=$(cd "$work/services/go-api" && pwd -P)
cat >"$work/bin/go" <<'SH'
#!/usr/bin/env bash
printf '%s %s\n' "$(pwd -P)" "$*" >>"$STUB_LOG"
printf '%s' "${STUB_OUT:-}"
exit "${STUB_RC:-0}"
SH
chmod +x "$work/bin/go"

run() {
  local dir=$1; shift
  (cd "$dir" && PATH="$work/bin:$PATH" STUB_LOG="$work/go.log" "$@" bash "$work/scripts/ci/go-deadcode.sh" 2>&1)
}
t() {
  local want=$1 name=$2 contains=$3 dir=$4; shift 4
  local out rc=0
  out=$(run "$dir" env "$@") || rc=$?
  if [ "$rc" != "$want" ] || ! grep -qF -- "$contains" <<<"$out"; then
    echo "FAIL $name: exit $rc (want $want), output: $out"
    fail=1
  fi
}

for dir in "$work" "$work/scripts" "$work/services/go-api/internal/deep/er" "$work/elsewhere"; do
  : >"$work/go.log"
  t 0 "a clean tree passes from ${dir#"$work"}" "" "$dir"
  grep -qxF "$module run golang.org/x/tools/cmd/deadcode@v0.53.0 -test ./..." "$work/go.log" || {
    echo "FAIL from ${dir#"$work"} deadcode did not run over the whole module: $(cat "$work/go.log")"
    fail=1
  }
done

t 1 "a finding fails and is shown" "unreachable func: Orphan" "$work/elsewhere" STUB_OUT="x/x.go:5:6: unreachable func: Orphan"
t 2 "a deadcode run that fails is not a clean tree" "nothing was checked" "$work/elsewhere" STUB_RC=1

rm "$work/services/go-api/go.mod"
t 2 "a missing module is an error, not a pass" "go.mod is missing, so nothing was checked" "$work/elsewhere"

[ "$fail" = 0 ] && echo "go-deadcode.test.sh: all cases pass"
exit "$fail"
