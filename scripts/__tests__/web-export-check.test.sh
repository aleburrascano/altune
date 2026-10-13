#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/web-export-check.sh
fail=0

scratch=$(mktemp -d); trap 'rm -rf "$scratch"' EXIT
mkdir -p "$scratch/bin"
cat >"$scratch/bin/npx" <<'STUB'
#!/usr/bin/env bash
{
  echo "npx $*"
  echo "cwd $(basename "$PWD")"
  echo "SUPABASE_URL=$EXPO_PUBLIC_SUPABASE_URL"
  echo "ANON_KEY=$EXPO_PUBLIC_SUPABASE_ANON_KEY"
  echo "API_URL=$EXPO_PUBLIC_API_URL"
  echo "OFFLINE=$EXPO_OFFLINE"
} >"$STUB_LOG"
out=""
while [ $# -gt 0 ]; do [ "$1" = --output-dir ] && out=$2; shift; done
[ "${STUB_EXIT:-0}" = 0 ] || exit "$STUB_EXIT"
[ "${STUB_NO_INDEX:-}" = 1 ] || : >"$out/index.html"
STUB
chmod +x "$scratch/bin/npx"
export STUB_LOG=$scratch/npx.log PATH="$scratch/bin:$PATH"
unset EXPO_PUBLIC_SUPABASE_URL EXPO_PUBLIC_SUPABASE_ANON_KEY EXPO_PUBLIC_API_URL EXPO_OFFLINE

OUT=""; CODE=0
run() { OUT=$(env "$@" bash "$script" 2>&1); CODE=$?; }
check() {
  [ "$CODE" = "$1" ] || { echo "FAIL want $1 got $CODE: $2"; sed 's/^/    /' <<<"$OUT"; fail=1; }
}
logged() { grep -qx -- "$1" "$STUB_LOG" || { echo "FAIL npx should see '$1'"; sed 's/^/    /' "$STUB_LOG"; fail=1; }; }

run
check 0 "a web export that writes index.html passes"
grep -q "ios and android skipped" <<<"$OUT" || { echo "FAIL should say ios and android are skipped"; fail=1; }
grep -q "web export ok" <<<"$OUT" || { echo "FAIL should report the web export ok"; fail=1; }
grep -q "^npx expo export -p web --clear --output-dir " "$STUB_LOG" || { echo "FAIL should run a clean web export into a temp dir"; fail=1; }
logged "cwd client"
logged "SUPABASE_URL=https://fixture.supabase.co"
logged "ANON_KEY=fixture-anon-key"
logged "API_URL=https://api.fixture.invalid"
logged "OFFLINE=1"

run EXPO_PUBLIC_API_URL=https://api.example.com
check 0 "a caller's own env wins"
logged "API_URL=https://api.example.com"

run STUB_EXIT=1
check 1 "a failing export fails"
grep -q "web export failed" <<<"$OUT" || { echo "FAIL should say the web export failed"; fail=1; }

run STUB_NO_INDEX=1
check 1 "an export with no index.html fails"

[ $fail = 0 ] && echo "web-export-check.test.sh: all cases pass"
exit $fail
