#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
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
    if [ -n "${STUB_GRAFT:-}" ]; then
      mkdir -p node_modules/@nanonets/graft/node_modules/tree-sitter node_modules/@nanonets/graft/node_modules/tree-sitter-go
    fi
    exit "${STUB_NPM_CI_EXIT:-0}"
    ;;
  rebuild) shift; echo "$*" >>"$STUB_REBUILD_LOG"; exit "${STUB_NPM_REBUILD_EXIT:-0}" ;;
esac
EOF
cat >"$stubbin/rm" <<'STUB'
#!/usr/bin/env bash
/bin/rm "$@"; rc=$?
if [ -n "${STUB_RACE_LINK:-}" ]; then
  for a in "$@"; do
    case $a in */node_modules) [ -e "$a" ] || ln -s "$STUB_RACE_LINK" "$a" ;; esac
  done
fi
exit $rc
STUB
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

rm -rf "$wt/node_modules" "$main/node_modules"
mkdir -p "$main/node_modules" && : >"$main/node_modules/.complete"
echo '{"added":"dep"}' >"$wt/package-lock.json"
export STUB_NPM_CI_EXIT=0
run .
check 0 "runs npm ci when the worktree's lockfile differs from the main checkout's"
[ -f "$wt/node_modules/.complete" ] && [ ! -L "$wt/node_modules" ] \
  || { echo "FAIL a lockfile that differs from main should trigger npm ci, not a symlink"; fail=1; }

rm -rf "$wt/node_modules"
ln -s "$main/node_modules" "$wt/node_modules"
rm -f "$main/node_modules/.complete"
run .
check 0 "replaces a stale borrowed symlink when the lockfile differs"
[ -f "$wt/node_modules/.complete" ] && [ ! -L "$wt/node_modules" ] \
  || { echo "FAIL a stale borrowed symlink should be replaced by a real node_modules"; fail=1; }
[ -d "$main/node_modules" ] || { echo "FAIL replacing the symlink must leave the main checkout's node_modules"; fail=1; }
unset STUB_NPM_CI_EXIT
git -C "$wt" checkout -q -- package-lock.json
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
export STUB_NPM_CI_EXIT=0 STUB_GRAFT=1 STUB_REBUILD_LOG="$scratch/rebuild.log"
run .
check 0 "builds graft's tree-sitter parsers after npm ci --ignore-scripts"
[ "$(cat "$STUB_REBUILD_LOG" 2>/dev/null)" = "tree-sitter tree-sitter-go" ] \
  || { echo "FAIL npm rebuild should get graft's parsers, got: $(cat "$STUB_REBUILD_LOG" 2>/dev/null)"; fail=1; }

rm -rf "$wt/node_modules"
export STUB_NPM_REBUILD_EXIT=1
run .
check 1 "exits 1 when building graft's parsers fails"
unset STUB_NPM_CI_EXIT STUB_GRAFT STUB_REBUILD_LOG STUB_NPM_REBUILD_EXIT

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

d22="$HOME/.nvm/versions/node/v22.23.3/bin"; d24="$HOME/.nvm/versions/node/v24.1.0/bin"
mkdir -p "$d22" "$d24"
cp "$stubbin/node" "$d22/node"; cp "$stubbin/node" "$d24/node"
rm -rf "$wt/node_modules"
export STUB_NPM_CI_EXIT=0 STUB_NODE_MAJOR=24
run .
check 0 "Node 24 on PATH with Node 22 under nvm"
grep -qx "export PATH=$d22:\$PATH" <<<"$OUT" \
  || { echo "FAIL should print the .nvmrc Node 22 PATH, not the Node 24 on PATH"; echo "$OUT" | sed 's/^/    /'; fail=1; }
echo 20 >"$wt/.nvmrc"
mkdir -p "$HOME/.nvm/versions/node/v20.5.0/bin" && cp "$stubbin/node" "$HOME/.nvm/versions/node/v20.5.0/bin/node"
run .
grep -qx "export PATH=$HOME/.nvm/versions/node/v20.5.0/bin:\$PATH" <<<"$OUT" \
  || { echo "FAIL should follow the major version in .nvmrc"; echo "$OUT" | sed 's/^/    /'; fail=1; }
rm -f "$wt/.nvmrc"
rm -rf "$HOME/.nvm/versions/node/v20.5.0" "$d22"
run .
check 0 "falls back to a newer Node when the .nvmrc version is missing"
grep -q "Node 22 (.nvmrc) not found" "$wt/err.log" || { echo "FAIL the fallback should say Node 22 is missing"; fail=1; }
unset STUB_NODE_MAJOR STUB_NPM_CI_EXIT
rm -rf "$HOME/.nvm"

mkdir -p "$wt/sub/dir" "$wt/client" "$wt/services/overseer/web" "$main/client/node_modules" "$main/services/overseer/web/node_modules"
: >"$wt/client/package-lock.json"; : >"$main/client/package-lock.json"
: >"$wt/services/overseer/web/package-lock.json"; : >"$main/services/overseer/web/package-lock.json"
: >"$main/client/node_modules/.complete"; : >"$main/services/overseer/web/node_modules/.complete"
rm -rf "$wt/node_modules"
mkdir -p "$wt/node_modules" && : >"$wt/node_modules/.complete"
OUT=$(cd "$wt/sub/dir" && bash "$wd" 2>"$wt/err.log"); CODE=$?
check 0 "runs from a subdirectory of the worktree"
[ -L "$wt/client/node_modules" ] || { echo "FAIL client node_modules should be linked from a subdirectory run"; fail=1; }
[ -L "$wt/services/overseer/web/node_modules" ] || { echo "FAIL the default set should link services/overseer/web too"; fail=1; }
[ -e "$wt/sub/dir/node_modules" ] && { echo "FAIL nothing should be linked into the current directory"; fail=1; }

rm -rf "$wt/client/node_modules"
STUB_RACE_LINK="$main/client/node_modules" run client
check 0 "a borrowed link made by a concurrent run between rm and ln is replaced"
{ [ -e "$main/client/node_modules/node_modules" ] || [ -L "$main/client/node_modules/node_modules" ]; } \
  && { echo "FAIL linking over an existing link must not create node_modules/node_modules in the main checkout"; fail=1; }
[ "$(readlink "$wt/client/node_modules")" = "$main/client/node_modules" ] \
  || { echo "FAIL client node_modules should still point at the main checkout"; fail=1; }
/bin/rm -rf "$main/client/node_modules/node_modules"

OUT=$(cd "$main" && bash "$wd" client 2>"$wt/err.log"); CODE=$?
check 0 "a run in the main checkout keeps a complete node_modules"
[ -d "$main/client/node_modules" ] && [ ! -L "$main/client/node_modules" ] \
  || { echo "FAIL the main checkout must keep its own node_modules, never a link to itself"; fail=1; }
rm -f "$main/client/node_modules/.complete"
export STUB_NPM_CI_EXIT=0
OUT=$(cd "$main" && bash "$wd" client 2>"$wt/err.log"); CODE=$?
check 0 "an incomplete node_modules in the main checkout is reinstalled, not borrowed from itself"
[ ! -L "$main/client/node_modules" ] && [ -f "$main/client/node_modules/.complete" ] \
  || { echo "FAIL the main checkout should npm ci, not link to itself"; fail=1; }
unset STUB_NPM_CI_EXIT
rm -rf "$wt/sub" "$wt/client" "$wt/services" "$main/client" "$main/services"

[ $fail = 0 ] && echo "worktree-deps.test.sh: all cases pass"
exit $fail
