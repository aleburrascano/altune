#!/usr/bin/env bash
cd "$(dirname "$0")" || exit 1
script=$PWD/e2e-stack.sh
fail=0

scratch=$(mktemp -d); trap 'rm -rf "$scratch"' EXIT
stubbin="$scratch/stubbin"; mkdir -p "$stubbin"
export E2E_STACK_DIR="$scratch/state"
export E2E_HEALTH_ATTEMPTS=1 E2E_READY_ATTEMPTS=1
export STUB_LOG="$scratch/calls.log"
export STUB_API_ENV="$scratch/api.env"

cat >"$stubbin/docker" <<'STUB'
#!/usr/bin/env bash
echo "docker $*" >>"$STUB_LOG"
case "$1" in
  run) echo "cid-$RANDOM" ;;
  port) echo "127.0.0.1:5555" ;;
esac
exit 0
STUB
cat >"$stubbin/go" <<'STUB'
#!/usr/bin/env bash
echo "go $*" >>"$STUB_LOG"
out=""
while [ $# -gt 0 ]; do [ "$1" = -o ] && out=$2; shift; done
printf '#!/usr/bin/env bash\nsleep 30\n' >"$out"; chmod +x "$out"
sed -i '1a env >"$STUB_API_ENV"' "$out"
STUB
cat >"$stubbin/npx" <<'STUB'
#!/usr/bin/env bash
echo "npx $* TEST_AUTH=$EXPO_PUBLIC_TEST_AUTH" >>"$STUB_LOG"
mkdir -p dist
STUB
cat >"$stubbin/python3" <<'STUB'
#!/usr/bin/env bash
if [ "$1" = -c ]; then echo "$((20000 + RANDOM % 1000))"; exit 0; fi
sleep 30
STUB
cat >"$stubbin/curl" <<'STUB'
#!/usr/bin/env bash
echo "curl $*" >>"$STUB_LOG"
[ "${STUB_HEALTH:-200}" = 200 ]
STUB
chmod +x "$stubbin"/*
export PATH="$stubbin:$PATH"

OUT=""; CODE=0
run() { OUT=$(bash "$script" "$@" 2>"$scratch/err.log"); CODE=$?; }
check() {
  [ "$CODE" = "$1" ] || { echo "FAIL want $1 got $CODE: $2"; sed 's/^/    /' "$scratch/err.log"; fail=1; }
}

run
check 2 "no argument prints usage and exits 2"
run sideways
check 2 "an unknown command exits 2"
run up down
check 2 "extra arguments exit 2"

run down
check 0 "down with nothing up is a no-op"

: >"$STUB_LOG"
run up
check 0 "up exits 0 once /health is 200"
for key in API_URL WEB_URL DATABASE_URL; do
  grep -q "^$key=" <<<"$OUT" || { echo "FAIL up should print $key"; fail=1; }
done
grep -q "^API_URL=http://127.0.0.1:[0-9]*$" <<<"$OUT" || { echo "FAIL API_URL should be a loopback URL"; fail=1; }
grep -q "^DATABASE_URL=postgres://altune:altune_dev@127.0.0.1:5555/altune" <<<"$OUT" || { echo "FAIL DATABASE_URL should use the mapped port"; fail=1; }
grep -q "expo export -p web --dev TEST_AUTH=1" "$STUB_LOG" || { echo "FAIL web export should run with --dev and test auth"; fail=1; }
[ -d "$E2E_STACK_DIR" ] || { echo "FAIL state should exist while the stack is up"; fail=1; }
[ "$(grep -c '^docker run' "$STUB_LOG")" = "$(grep -c '^docker run .*--label altune-ci=1 ' "$STUB_LOG")" ] || { echo "FAIL every docker run should carry the altune-ci label"; fail=1; }

sleep 1
for want in PROVIDER_REPLAY_ENABLED=true PROVIDER_REPLAY_DIR=.*/apps/mobile/e2e/fixtures/providers \
  SPOTIFY_ENABLED=false SOUNDCLOUD_ENABLED=false APPLEMUSIC_ENABLED=false \
  AMAZONMUSIC_ENABLED=false YTMUSIC_ENABLED=false YTDLP_ENABLED=false; do
  grep -q "^$want$" "$STUB_API_ENV" || { echo "FAIL api should start with $want"; fail=1; }
done

run up
check 1 "a second up refuses while the first is running"

pids=$(cat "$E2E_STACK_DIR"/*.pid)
: >"$STUB_LOG"
run down
check 0 "down exits 0"
[ ! -e "$E2E_STACK_DIR" ] || { echo "FAIL down should remove the state dir"; fail=1; }
[ "$(grep -c '^docker rm -f' "$STUB_LOG")" = 2 ] || { echo "FAIL down should remove both containers"; fail=1; }
[ "$(grep -c '^docker rm -fv' "$STUB_LOG")" = 2 ] || { echo "FAIL down should remove both containers with their volumes"; fail=1; }
for pid in $pids; do
  kill -0 "$pid" 2>/dev/null && { echo "FAIL down should stop process $pid"; fail=1; }
done

export STUB_HEALTH=500
: >"$STUB_LOG"
run up
check 1 "up exits non-zero when /health never returns 200"
[ ! -e "$E2E_STACK_DIR" ] || { echo "FAIL a failed up should clean up after itself"; fail=1; }
grep -q '^docker rm -f' "$STUB_LOG" || { echo "FAIL a failed up should remove its containers"; fail=1; }
grep -q '^docker rm -fv' "$STUB_LOG" || { echo "FAIL a failed up should remove its containers with their volumes"; fail=1; }
grep -q "API_URL=" <<<"$OUT" && { echo "FAIL a failed up should not publish API_URL"; fail=1; }
unset STUB_HEALTH

[ $fail = 0 ] && echo "e2e-stack.test.sh: all cases pass"
exit $fail
