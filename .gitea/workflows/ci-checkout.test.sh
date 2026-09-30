#!/usr/bin/env bash
cd "$(dirname "$0")" || exit 1
script=$PWD/ci-checkout.sh
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

repo=$work/repo
git init -qb main "$repo"
git -C "$repo" -c user.email=t@example.com -c user.name=t commit -q --allow-empty -m base
mkdir "$work/scratch"
: >"$work/env"; : >"$work/path"

run() {
  ALTUNE_REPO=$repo TMPDIR=$work/scratch GITHUB_ENV=$work/env GITHUB_PATH=$work/path bash "$script" "$1" >"$work/out.log" 2>&1
}

if run main; then
  grep -q "^WT=$work/scratch/ci-wf\." "$work/env" || { echo "FAIL good ref: no WT under TMPDIR"; fail=1; }
else
  echo "FAIL good ref: exited non-zero"; sed 's/^/    /' "$work/out.log"; fail=1
fi

git -C "$repo" worktree remove --force "$(sed -n 's/^WT=//p' "$work/env")" 2>/dev/null
rm -rf "$work/scratch"; mkdir "$work/scratch"

if run no-such-ref; then
  echo "FAIL missing ref: exited zero"; fail=1
fi
[ -z "$(ls -A "$work/scratch")" ] || { echo "FAIL missing ref: left behind: $(ls -A "$work/scratch")"; fail=1; }

[ "$fail" = 0 ] && echo "ci-checkout: all passed"
exit "$fail"
