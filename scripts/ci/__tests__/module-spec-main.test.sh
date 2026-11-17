#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/module-spec-main.sh
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

remote=$work/remote.git
repo=$work/repo
git init -q --bare -b main "$remote"
git init -qb main "$repo"
mkdir -p "$repo/scripts/ci" "$repo/client/module-spec.baseline.d"
cp "$script" "$repo/scripts/ci/"
cat >"$repo/scripts/module-spec.sh" <<'SH'
#!/usr/bin/env bash
echo "kit $*" >>"$STUB_LOG"
case "$1 ${2:-}" in
  "check --since") exit "${CHECK_RC:-0}" ;;
  "baseline --list") [ -z "${LIST_FAIL:-}" ] || { echo "kit exploded" >&2; exit 1; }; echo "== client" >&2; cat "$STUB_KEYS" ;;
  "baseline --compact")
    case $COMPACT in
      none) ;;
      fold) rm client/module-spec.baseline.d/*.json && echo '{"keys":["a","b"]}' >client/module-spec.baseline.json ;;
      lose) rm client/module-spec.baseline.d/*.json && echo a >"$STUB_KEYS" ;;
      stray) rm client/module-spec.baseline.d/*.json && echo oops >client/stray.ts ;;
    esac ;;
esac
SH
echo '{"keys":["a","b","c"]}' >"$repo/client/module-spec.baseline.json"
echo '{"removed":["c"]}' >"$repo/client/module-spec.baseline.d/one.json"
git -C "$repo" add -A
git -C "$repo" -c user.email=t@example.com -c user.name=t commit -qm "Merge pull request 'feat(x): y' (#41) from feat/x into main"
git -C "$repo" remote add gitea "$remote"
git -C "$repo" push -q gitea HEAD:refs/heads/main
sha=$(git -C "$repo" rev-parse HEAD)

cat >"$work/forge" <<'STUB'
#!/usr/bin/env bash
echo "forge $*" >>"$STUB_LOG"
case "$*" in *"pr list"*) echo "${OPEN_PRS:-[]}" ;; esac
STUB
chmod +x "$work/forge"

export STUB_LOG=$work/log STUB_KEYS=$work/keys FORGE=$work/forge GITHUB_REPOSITORY=owner/repo
OUT=""; CODE=0
run() {
  rm -rf "$work/case"; git clone -q "$remote" "$work/case" && git -C "$work/case" remote rename origin gitea
  printf 'a\nb\n' >"$STUB_KEYS"; : >"$STUB_LOG"
  OUT=$(cd "$work/case" && bash scripts/ci/module-spec-main.sh "$@" 2>&1); CODE=$?
}
expect() {
  local want=$1 desc=$2 pat=${3:-}
  [ "$CODE" = "$want" ] || { echo "FAIL want exit $want got $CODE: $desc"; sed 's/^/    /' <<<"$OUT"; fail=1; return; }
  [ -z "$pat" ] || grep -qE "$pat" <<<"$OUT" || { echo "FAIL output lacks /$pat/: $desc"; sed 's/^/    /' <<<"$OUT"; fail=1; }
}
logged() { grep -qE "$1" "$STUB_LOG" || { echo "FAIL $2: log lacks /$1/"; sed 's/^/    /' "$STUB_LOG"; fail=1; }; }
not_logged() { ! grep -qE "$1" "$STUB_LOG" || { echo "FAIL $2: log has /$1/"; sed 's/^/    /' "$STUB_LOG"; fail=1; }; }
branches() { git -C "$remote" for-each-ref --format='%(refname:short)' refs/heads | grep -v '^main$'; }

run; expect 2 "no command prints usage" "usage: module-spec-main.sh"

CHECK_RC=0 run check; expect 0 "green check" "green at $sha"
logged '^kit check --since HEAD$' "check measures from HEAD, not from a local main that can be stale"

CHECK_RC=1 run check; expect 1 "red check names the commit" "::error::.*red at $sha \(Merge pull request 'feat\(x\): y' \(#41\)"
grep -q "git checkout $sha && bash scripts/module-spec.sh check --since HEAD" <<<"$OUT" || { echo "FAIL red check: no reproduce line"; fail=1; }

CHECK_RC=3 run check; expect 3 "a kit that cannot run is not blamed on the commit" "could not run module-spec check"
grep -q "is red" <<<"$OUT" && { echo "FAIL kit missing: blamed the commit"; fail=1; }

COMPACT=none run compact; expect 0 "nothing to fold" "no baseline fragments to compact"

COMPACT="fold" run compact; expect 0 "fold without OPEN_PR stays local" "nothing is pushed"
not_logged '^forge' "fold without OPEN_PR"
[ -z "$(branches)" ] || { echo "FAIL fold without OPEN_PR pushed $(branches)"; fail=1; }

LIST_FAIL=1 COMPACT="fold" run compact; expect 1 "a baseline --list that fails" "kit exploded"
grep -q "baseline --list failed" <<<"$OUT" || { echo "FAIL a baseline --list that fails: does not say so"; fail=1; }
not_logged '^kit baseline --compact' "a baseline --list that fails"

COMPACT=lose run compact; expect 1 "a compact that changes the keys" "changed what the baseline holds"
COMPACT=stray OPEN_PR=true run compact; expect 1 "a compact that touches other files" "outside the baseline: client/stray.ts"
[ -z "$(branches)" ] || { echo "FAIL refused compaction pushed $(branches)"; fail=1; }

rm -rf "$work/dirty"; git clone -q "$remote" "$work/dirty"; echo x >"$work/dirty/client/wip.ts"
OUT=$(cd "$work/dirty" && COMPACT="fold" bash scripts/ci/module-spec-main.sh compact 2>&1); CODE=$?
expect 1 "a dirty checkout" "uncommitted changes"

COMPACT="fold" OPEN_PRS='[{"number":7,"headRefName":"tooling/module-spec-compact-abc"}]' OPEN_PR=true run compact
expect 1 "an open compaction PR" "PR #7 is still open"
not_logged 'pr create' "an open compaction PR"

git -C "$repo" -c user.email=t@example.com -c user.name=t commit -q --allow-empty -m "off main"
git -C "$repo" push -q gitea HEAD:refs/heads/side
rm -rf "$work/case"; git clone -q -b side "$remote" "$work/case"; git -C "$work/case" remote rename origin gitea; : >"$STUB_LOG"; printf 'a\nb\n' >"$STUB_KEYS"
OUT=$(cd "$work/case" && COMPACT="fold" OPEN_PR=true bash scripts/ci/module-spec-main.sh compact 2>&1); CODE=$?
expect 1 "a commit not on main" "is not on gitea/main"
not_logged 'pr create' "a commit not on main"
git -C "$remote" branch -qD side

orphan=$(git -C "$repo" -c user.email=t@example.com -c user.name=t commit-tree "$sha^{tree}" -p "$sha" -m "an earlier run that opened no PR")
git -C "$repo" push -q gitea "$orphan:refs/heads/tooling/module-spec-compact-$(git -C "$repo" rev-parse --short=9 "$sha")"
COMPACT="fold" OPEN_PR=true RUN_URL=http://ci/run/9 run compact; expect 0 "fold and open the PR, over a branch an earlier run pushed without a PR"
pushed=$(branches)
[ "$pushed" = "tooling/module-spec-compact-$(git -C "$repo" rev-parse --short=9 "$sha")" ] || { echo "FAIL pushed branch is '$pushed'"; fail=1; }
if [ -n "$pushed" ]; then
  [ "$(git -C "$remote" rev-parse "$pushed")" != "$orphan" ] || { echo "FAIL the earlier run's branch was not replaced"; fail=1; }
  [ "$(git -C "$remote" rev-parse "$pushed^")" = "$sha" ] || { echo "FAIL the PR commit does not sit on main"; fail=1; }
  [ "$(git -C "$remote" diff --name-only "$sha" "$pushed" | sort | tr '\n' ' ')" = "client/module-spec.baseline.d/one.json client/module-spec.baseline.json " ] || { echo "FAIL the PR commit holds more than the baseline"; fail=1; }
fi
logged "^forge pr create -R owner/repo --base main --head $pushed --title tooling\(module-spec\): fold 1 baseline fragments" "fold and open the PR"
logged "Run: http://ci/run/9" "the PR body links the run"

[ "$fail" = 0 ] && echo "module-spec-main: all passed"
exit "$fail"
