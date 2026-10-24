#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/ota-runtime.sh
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin" "$work/shared/node_modules/pkg"
cat > "$work/bin/npx" <<'STUB'
#!/usr/bin/env bash
[ -L node_modules ] && { echo '{"runtimeVersion":"linked"}'; exit 0; }
echo "{\"runtimeVersion\":\"$FAKE_RUNTIME\"}"
STUB
cat > "$work/bin/gh" <<'STUB'
#!/usr/bin/env bash
jq -n --arg body "Over-the-air runtime: \`$SHIPPED\`. Updates published for this runtime reach this build." '{tagName:"v2.2.1",body:$body}'
STUB
chmod +x "$work/bin/npx" "$work/bin/gh"
fail() { echo "FAIL $1"; exit 1; }
run() {
  rm -rf "$work/client" && mkdir -p "$work/client" && ln -s "$work/shared/node_modules" "$work/client/node_modules"
  (cd "$work/client" && PATH="$work/bin:$PATH" FAKE_RUNTIME=$1 SHIPPED=$2 bash "$script" 2>"$work/err")
}
out=$(run ae2a ae2a) || fail "a runtime matching the latest iOS release was rejected: $(cat "$work/err")"
[ "$out" = ae2a ] || fail "printed '$out', not the runtime"
[ -L "$work/client/node_modules" ] && fail "node_modules stayed a link, so the fingerprint hashes paths outside the checkout"
[ -d "$work/client/node_modules/pkg" ] || fail "the copied node_modules lost its packages"
run 3c7b ae2a >/dev/null && fail "a runtime no iOS release runs was accepted"
grep -q "v2.2.1 runs ae2a" "$work/err" || fail "the mismatch error does not name the release and its runtime: $(cat "$work/err")"
echo "ota-runtime tests ok"
