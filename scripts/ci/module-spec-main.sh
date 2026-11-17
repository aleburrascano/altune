#!/usr/bin/env bash
set -euo pipefail
usage="usage: module-spec-main.sh check | compact   (compact pushes a branch and opens a PR only with OPEN_PR=true)"
cd "$(git rev-parse --show-toplevel)"

fail() { echo "::error::module-spec-main: $*" >&2; exit 1; }
retry() { "$@" || { sleep 5; "$@"; } || { sleep 5; "$@"; }; }

check() {
  local sha subject rc=0
  sha=$(git rev-parse HEAD)
  subject=$(git log -1 --format=%s HEAD)
  bash scripts/module-spec.sh check --since HEAD || rc=$?
  case $rc in
    0) echo "module-spec-main: green at $sha ($subject)" ;;
    1) fail "module-spec check is red at $sha ($subject). The lines above, under each root, say what broke: violations, which PRs that each passed can merge into, or a kit error such as a dependency that did not install. Reproduce: git fetch gitea && git checkout $sha && bash scripts/module-spec.sh check --since HEAD" ;;
    *) echo "::error::module-spec-main: could not run module-spec check at $sha (exit $rc), see above" >&2; exit "$rc" ;;
  esac
}

list() {
  local out
  out=$(bash scripts/module-spec.sh baseline --list 2>&1) || { echo "$out" >&2; fail "baseline --list failed (above), so compact cannot prove it keeps the baseline"; }
  echo "$out"
}

compact() {
  local before after changed stray fragments title open
  [ -z "$(git status --porcelain)" ] || fail "the checkout has uncommitted changes; compact runs on a clean one so the PR holds only the compaction"
  before=$(list)
  bash scripts/module-spec.sh baseline --compact
  after=$(list)
  if [ "$before" != "$after" ]; then
    diff <(echo "$before") <(echo "$after") >&2 || true
    fail "baseline --compact changed what the baseline holds (diff above), so nothing is pushed"
  fi
  changed=$(git status --porcelain --untracked-files=all | cut -c4-)
  [ -n "$changed" ] || { echo "module-spec-main: no baseline fragments to compact"; return 0; }
  stray=$(grep -vE '(^|/)module-spec\.baseline\.(json$|d/)' <<<"$changed" || true)
  [ -z "$stray" ] || fail "baseline --compact touched files outside the baseline: $stray"
  git status --short
  if [ "${OPEN_PR:-false}" != true ]; then
    echo "module-spec-main: compacted, the baseline is unchanged; OPEN_PR is not true, so nothing is pushed"
    return 0
  fi

  local remote=${REMOTE:-gitea} repo=${GITHUB_REPOSITORY:?OPEN_PR=true needs GITHUB_REPOSITORY, the owner/name to open the PR on}
  local forge=${FORGE:-$(command -v forge || echo /home/ubuntu/.local/bin/forge)}
  retry git fetch -q "$remote" main
  git merge-base --is-ancestor HEAD FETCH_HEAD || fail "$(git rev-parse --short HEAD) is not on $remote/main; a compaction PR starts from main only"
  open=$(retry "$forge" pr list -R "$repo" --state open --json number,headRefName | jq -r '.[] | select(.headRefName | startswith("tooling/module-spec-compact-")) | .number')
  [ -z "$open" ] || fail "compaction PR #$open is still open; it should have merged on green, so read its checks before compacting again"

  fragments=$(grep -cE '(^|/)module-spec\.baseline\.d/' <<<"$changed" || true)
  title="tooling(module-spec): fold $fragments baseline fragments into module-spec.baseline.json; baseline unchanged"
  local branch body
  branch=tooling/module-spec-compact-$(git rev-parse --short=9 HEAD)
  body="The weekly compaction ran \`bash scripts/module-spec.sh baseline --compact\` on main at $(git rev-parse HEAD). \`baseline --list\` printed the same output before and after, so the baseline holds the same keys; only where they are stored changed.${RUN_URL:+ Run: $RUN_URL}"
  git add -A
  git -c user.name=gitea-actions -c user.email=actions@altune-git.duckdns.org -c core.hooksPath=/dev/null commit -q -m "$title"
  retry git push -q --force "$remote" "HEAD:refs/heads/$branch"
  retry "$forge" pr create -R "$repo" --base main --head "$branch" --title "$title" --body "$body"
}

case ${1:-} in
  check) check ;;
  compact) compact ;;
  *) echo "$usage" >&2; exit 2 ;;
esac
