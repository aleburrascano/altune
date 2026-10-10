#!/usr/bin/env bash
cd "$(dirname "$0")" || exit 1
script=$PWD/ci-node-deps.sh
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

mkdir -p "$work/bin"
cat >"$work/bin/npm" <<'STUB'
#!/usr/bin/env bash
echo "npm $* in $PWD legacy-peer-deps=$(cat .npmrc 2>/dev/null)" >>"$NPM_LOG"
mkdir -p node_modules
STUB
chmod +x "$work/bin/npm"
export NPM_LOG=$work/npm.log PATH="$work/bin:$PATH"

repo=$work/repo
wt=$work/wt
lock='{"lockfileVersion":3,"packages":{"":{"name":"x"},"node_modules/a":{"version":"1.0.0"}}}'

reset() {
  rm -rf "$repo" "$wt" "$NPM_LOG"
  mkdir -p "$repo/client/node_modules/a" "$wt/client"
  echo "$lock" >"$repo/client/package-lock.json"
  echo "$lock" >"$wt/client/package-lock.json"
  echo legacy-peer-deps=true >"$wt/client/.npmrc"
}

run() { ALTUNE_REPO=$repo WT=$wt bash "$script" client >"$work/out.log" 2>&1; }

reset
run || { echo "FAIL matching lockfiles: exited non-zero"; fail=1; }
[ "$(readlink "$wt/client/node_modules")" = "$repo/client/node_modules" ] || { echo "FAIL matching lockfiles should link the main node_modules"; fail=1; }

run || { echo "FAIL second run: exited non-zero"; fail=1; }
[ -e "$repo/client/node_modules/node_modules" ] || [ -L "$repo/client/node_modules/node_modules" ] \
  && { echo "FAIL a second run must not create node_modules/node_modules inside the main checkout"; fail=1; }

reset
ln -s "$work/gone" "$wt/client/node_modules"
run || { echo "FAIL a dangling link: exited non-zero"; sed 's/^/    /' "$work/out.log"; fail=1; }
[ "$(readlink "$wt/client/node_modules")" = "$repo/client/node_modules" ] || { echo "FAIL a dangling node_modules link should be replaced"; fail=1; }

reset
echo '{"lockfileVersion":3,"packages":{"":{"name":"x"},"node_modules/a":{"version":"2.0.0"}}}' >"$wt/client/package-lock.json"
run || { echo "FAIL differing lockfiles: exited non-zero"; fail=1; }
grep -q "^npm ci .* in $wt/client legacy-peer-deps=legacy-peer-deps=true$" "$NPM_LOG" \
  || { echo "FAIL differing lockfiles should npm ci inside client, where client/.npmrc applies"; cat "$NPM_LOG" 2>/dev/null | sed 's/^/    /'; fail=1; }
[ -L "$wt/client/node_modules" ] && { echo "FAIL differing lockfiles must not link"; fail=1; }

[ $fail = 0 ] && echo "ci-node-deps.test.sh: all cases pass"
exit $fail
