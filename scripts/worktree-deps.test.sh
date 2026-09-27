#!/usr/bin/env bash
cd "$(dirname "$0")" || exit 1
wd=$PWD/worktree-deps.sh
fail=0

scratch=$(mktemp -d); trap 'rm -rf "$scratch"' EXIT
export HOME="$scratch/home"; mkdir -p "$HOME"
stubbin="$scratch/stubbin"; mkdir -p "$stubbin"

cat >"$stubbin/node" <<'EOF'
#!/usr/bin/env bash
if [ "$1" = "-e" ]; then
  echo "${STUB_NODE_MAJOR:-22}"
  exit 0
fi
echo "v${STUB_NODE_MAJOR:-22}.0.0"
EOF

cat >"$stubbin/npm" <<'EOF'
#!/usr/bin/env bash
case "$1" in
  ls) [ -f node_modules/.complete ] ;;
  ci)
    rm -rf node_modules
    if [ "${STUB_NPM_CI_EXIT:-0}" = 0 ]; then
      mkdir node_modules && : >node_modules/.complete
    fi
    exit "${STUB_NPM_CI_EXIT:-0}"
    ;;
esac
EOF
chmod +x "$stubbin"/*
export PATH="$stubbin:$PATH"

main=$scratch/main
git init -q "$main" && git -C "$main" config user.email t@t && git -C "$main" config user.name t
: >"$main/package-lock.json"
git -C "$main" add -A && git -C "$main" commit -qm base

wt=$scratch/wt
git -C "$main" worktree add -q -B wt "$wt" >/dev/null

OUT=""; CODE=0
run() {
  OUT=$(cd "$wt" && bash "$wd" "$@" 2>"$wt/err.log"); CODE=$?
}

check() {
  [ "$CODE" = "$1" ] || {
    echo "FAIL want $1 got $CODE: $2"
    sed 's/^/    /' "$wt/err.log"
    fail=1
  }
}

rm -rf "$wt/node_modules"
mkdir -p "$wt/node_modules" && : >"$wt/node_modules/.complete"
export STUB_NPM_CI_EXIT=1
run .
check 0 "a complete node_modules is kept as-is"
[ -L "$wt/node_modules" ] && { echo "FAIL a kept node_modules should not be a symlink"; fail=1; }
unset STUB_NPM_CI_EXIT

rm -rf "$wt/node_modules" "$main/node_modules"
mkdir -p "$main/node_modules" && : >"$main/node_modules/.complete"
run .
check 0 "a complete main-checkout node_modules is symlinked"
[ -L "$wt/node_modules" ] || { echo "FAIL borrowing should symlink node_modules"; fail=1; }
[ "$(readlink -f "$wt/node_modules")" = "$(readlink -f "$main/node_modules")" ] \
  || { echo "FAIL symlink should point at the main checkout's node_modules"; fail=1; }
grep -qx node_modules "$main/.git/info/exclude" 2>/dev/null \
  || { echo "FAIL node_modules should be added to git info/exclude"; fail=1; }
rm -rf "$main/node_modules"

rm -rf "$wt/node_modules" "$main/node_modules"
mkdir -p "$main/node_modules"
export STUB_NPM_CI_EXIT=0
run .
check 0 "runs npm ci when the main checkout's node_modules is present but stale"
[ -f "$wt/node_modules/.complete" ] && [ ! -L "$wt/node_modules" ] \
  || { echo "FAIL a stale main node_modules should trigger npm ci, not a symlink"; fail=1; }
unset STUB_NPM_CI_EXIT
rm -rf "$main/node_modules"

rm -rf "$wt/node_modules"
export STUB_NPM_CI_EXIT=0
run .
check 0 "falls back to npm ci when neither node_modules is complete"
[ -f "$wt/node_modules/.complete" ] && [ ! -L "$wt/node_modules" ] \
  || { echo "FAIL npm ci fallback should leave a real node_modules"; fail=1; }

rm -rf "$wt/node_modules"
export STUB_NPM_CI_EXIT=1
run .
check 1 "exits 1 when npm ci fails"
unset STUB_NPM_CI_EXIT

rm -rf "$wt/node_modules"
export STUB_NODE_MAJOR=20
run .
check 3 "exits 3 when no Node >= 22 is found anywhere"
unset STUB_NODE_MAJOR

d="$HOME/.nvm/versions/node/v22.9.0/bin"
mkdir -p "$d" && cp "$stubbin/node" "$d/node"
rm -rf "$wt/node_modules"
export STUB_NPM_CI_EXIT=0
export STUB_NODE_MAJOR=20
run .
check 0 "no Node >= 22 on PATH, but the newest one under nvm is used"
grep -qx "export PATH=$d:\$PATH" <<<"$OUT" \
  || { echo "FAIL should print the nvm bin dir's PATH export line"; echo "$OUT" | sed 's/^/    /'; fail=1; }
unset STUB_NODE_MAJOR STUB_NPM_CI_EXIT
rm -rf "$HOME/.nvm"

rm -rf "$wt/node_modules"
run no-such-dir
check 0 "a dir without package-lock.json is skipped"
grep -q '^export PATH=' <<<"$OUT" || { echo "FAIL should still print the PATH export line"; fail=1; }

[ $fail = 0 ] && echo "worktree-deps.test.sh: all cases pass"
exit $fail
