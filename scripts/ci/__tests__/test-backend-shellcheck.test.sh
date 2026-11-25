#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/test-backend-shellcheck.sh
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

mkdir -p "$work/scripts/ci" "$work/services/go-api/deploy" "$work/bin" "$work/elsewhere"
cp "$script" "$work/scripts/ci/"
: >"$work/services/go-api/deploy/a.sh"
: >"$work/services/go-api/deploy/duckdns"
printf '#!/usr/bin/env bash\nprintf "%%s\\n" "$*" >>"$STUB_LOG"\n' >"$work/bin/shellcheck"
chmod +x "$work/bin/shellcheck"

for dir in "$work" "$work/services/go-api" "$work/elsewhere"; do
  : >"$work/shellcheck.log"
  out=$(cd "$dir" && PATH="$work/bin:$PATH" STUB_LOG="$work/shellcheck.log" bash "$work/scripts/ci/test-backend-shellcheck.sh" 2>&1) || { echo "FAIL from ${dir#"$work"}: $out"; fail=1; continue; }
  [ "$(cat "$work/shellcheck.log")" = "-x --exclude=SC2148 deploy/a.sh deploy/duckdns" ] || { echo "FAIL from ${dir#"$work"} shellcheck did not get the deploy scripts: $(cat "$work/shellcheck.log")"; fail=1; }
done

: >"$work/shellcheck.log"
out=$(cd "$work" && CDPATH="$work" PATH="$work/bin:$PATH" STUB_LOG="$work/shellcheck.log" bash scripts/ci/test-backend-shellcheck.sh 2>&1) || { echo "FAIL with CDPATH set: $out"; fail=1; }
[ "$(cat "$work/shellcheck.log")" = "-x --exclude=SC2148 deploy/a.sh deploy/duckdns" ] || { echo "FAIL with CDPATH set shellcheck did not get the deploy scripts: $(cat "$work/shellcheck.log")"; fail=1; }

rm -rf "$work/services"
out=$(cd "$work/elsewhere" && PATH="$work/bin:$PATH" STUB_LOG="$work/shellcheck.log" bash "$work/scripts/ci/test-backend-shellcheck.sh" 2>&1) && rc=0 || rc=$?
{ [ "$rc" = 2 ] && grep -qF "services/go-api was not found" <<<"$out"; } || { echo "FAIL a missing module folder: exit $rc, output: $out"; fail=1; }

[ "$fail" = 0 ] && echo "test-backend-shellcheck.test.sh: all cases pass"
exit "$fail"
