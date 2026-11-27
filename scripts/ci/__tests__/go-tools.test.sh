#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/go-tools.sh
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

mkdir -p "$work/bin"
cat >"$work/bin/go" <<'SH'
#!/usr/bin/env bash
if [ "$1" = env ]; then
  echo "$STUB_HOME"
  exit 0
fi
printf 'go %s\n' "$*" >>"$STUB_LOG"
n=$(grep -c "^go $*\$" "$STUB_LOG")
if [ "$n" -le "${STUB_GO_FAILS:-0}" ]; then
  echo "stub go: network is down" >&2
  exit 1
fi
SH
cat >"$work/bin/curl" <<'SH'
#!/usr/bin/env bash
printf 'curl\n' >>"$STUB_LOG"
n=$(grep -c '^curl$' "$STUB_LOG")
if [ "$n" -le "${STUB_CURL_FAILS:-0}" ]; then
  echo "stub curl: HTTP status 504" >&2
  exit 22
fi
echo "true"
SH
cat >"$work/bin/sh" <<'SH'
#!/usr/bin/env bash
cat >/dev/null
printf 'sh %s\n' "$*" >>"$STUB_LOG"
SH
cat >"$work/bin/sleep" <<'SH'
#!/usr/bin/env bash
printf 'sleep %s\n' "$*" >>"$STUB_LOG"
SH
chmod +x "$work/bin/go" "$work/bin/curl" "$work/bin/sh" "$work/bin/sleep"

run() {
  : >"$work/log"
  PATH="$work/bin:$PATH" STUB_HOME="$work/home" STUB_LOG="$work/log" env "$@" bash "$script" 2>&1
}
t() {
  local want=$1 name=$2 contains=$3; shift 3
  local out rc=0
  out=$(run "$@") || rc=$?
  if [ "$rc" != "$want" ] || ! grep -qF -- "$contains" <<<"$out"; then
    echo "FAIL $name: exit $rc (want $want), output: $out"
    fail=1
  fi
}
count() { grep -c -- "$1" "$work/log" || true; }

t 0 "a clean run installs all three tools" "" STUB_CURL_FAILS=0
[ "$(count '^go install')" = 2 ] || { echo "FAIL a clean run did not run both go installs: $(cat "$work/log")"; fail=1; }
[ "$(count '^sleep')" = 0 ] || { echo "FAIL a clean run slept: $(cat "$work/log")"; fail=1; }

t 0 "an install that fails twice then succeeds passes" "attempt 2 of 4" STUB_CURL_FAILS=2 STUB_GO_FAILS=2
[ "$(count '^curl')" = 3 ] || { echo "FAIL curl ran $(count '^curl') times, want 3"; fail=1; }
[ "$(grep -c '^sleep' "$work/log")" -ge 2 ] || { echo "FAIL no backoff between attempts: $(cat "$work/log")"; fail=1; }
[ "$(grep -x 'sleep [0-9]*' "$work/log" | head -2 | tr '\n' ',')" = "sleep 5,sleep 15," ] || { echo "FAIL backoff was not 5 then 15 seconds: $(cat "$work/log")"; fail=1; }

t 1 "a curl that always fails exits non-zero and says why" "golangci-lint failed after 4 attempts, giving up" STUB_CURL_FAILS=99
[ "$(count '^curl')" = 4 ] || { echo "FAIL curl ran $(count '^curl') times, want 4"; fail=1; }
grep -qx 'sleep 30' "$work/log" || { echo "FAIL the third backoff was not 30 seconds: $(cat "$work/log")"; fail=1; }
[ "$(count '^go install')" = 0 ] || { echo "FAIL later installs ran after the first gave up"; fail=1; }

t 1 "a go install that always fails exits non-zero and shows the error" "stub go: network is down" STUB_GO_FAILS=99
t 1 "a go install that always fails names the tool" "govulncheck failed after 4 attempts" STUB_GO_FAILS=99

t 0 "the backoff can be shortened" "" GO_TOOLS_BACKOFF="0 0 0" STUB_CURL_FAILS=1
grep -qx 'sleep 0' "$work/log" || { echo "FAIL GO_TOOLS_BACKOFF was ignored: $(cat "$work/log")"; fail=1; }

exit "$fail"
