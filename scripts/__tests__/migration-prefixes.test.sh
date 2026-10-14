#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/migration-prefixes.sh
fail=0

scratch=$(mktemp -d); trap 'rm -rf "$scratch"' EXIT
repo=$scratch/repo
mig=services/go-api/migrations

OUT=""; CODE=0
run() { OUT=$(cd "$repo" && bash "$script" "$@" 2>&1); CODE=$?; }
check() {
  [ "$CODE" = "$1" ] || { echo "FAIL want $1 got $CODE: $2"; sed 's/^/    /' <<<"$OUT"; fail=1; }
}
expect_out() {
  grep -qF -- "$1" <<<"$OUT" || { echo "FAIL output should mention '$1'"; sed 's/^/    /' <<<"$OUT"; fail=1; }
}

git init -q "$repo" && git -C "$repo" config user.email t@t && git -C "$repo" config user.name t
mkdir -p "$repo/$mig"
for f in 001_baseline 002_indexes 030_confidence; do : >"$repo/$mig/$f.sql"; done
git -C "$repo" add -A && git -C "$repo" commit -qm base

run "$mig"
check 0 "unique number prefixes pass"

: >"$repo/$mig/002_other.sql"
run "$mig"
check 1 "two files sharing a number prefix fail"
expect_out "number 002 is taken by more than one migration: 002_indexes.sql 002_other.sql"
rm "$repo/$mig/002_other.sql"

: >"$repo/$mig/031_acquisition_job_queue_available_at_index.sql"
: >"$repo/$mig/031_acquisition_run_attempts.sql"
run "$mig"
check 0 "the known 031 pair is allowed"
expect_out "allowing the known 031 pair"
expect_out "schema_migrations by its full file name"

: >"$repo/$mig/031_third.sql"
run "$mig"
check 1 "a third file joining the known 031 pair fails"
expect_out "number 031 is taken by more than one migration"
rm "$repo/$mig/031_third.sql"

rm "$repo/$mig/031_acquisition_run_attempts.sql"
: >"$repo/$mig/031_something_else.sql"
run "$mig"
check 1 "a different 031 pair is not the known pair and fails"
rm "$repo/$mig/031_something_else.sql" "$repo/$mig/031_acquisition_job_queue_available_at_index.sql"

git -C "$repo" checkout -qb other
: >"$repo/$mig/034_from_main.sql"
git -C "$repo" add -A && git -C "$repo" commit -qm "main took 034"
git -C "$repo" checkout -q -
: >"$repo/$mig/034_from_this_branch.sql"
run "$mig"
check 0 "without a ref only the working tree counts"
run "$mig" other
check 1 "a number already taken at the base ref fails, so concurrent PRs collide before merge"
expect_out "034_from_main.sql 034_from_this_branch.sql"
expect_out "rebase first"
rm "$repo/$mig/034_from_this_branch.sql"

run "$mig" other
check 0 "the base ref's own files do not collide with themselves"

git -C "$repo" checkout -q other
: >"$repo/$mig/035_clash.sql"
git -C "$repo" add -A && git -C "$repo" commit -qm "main has 035_clash"
git -C "$repo" checkout -qb renamer
git -C "$repo" mv "$mig/035_clash.sql" "$mig/036_clash.sql"
: >"$repo/$mig/035_other.sql"
git -C "$repo" add -A && git -C "$repo" commit -qm "renumber 035_clash"
run "$mig" other
check 0 "a file this branch renamed away does not count at the base ref"
git -C "$repo" checkout -q other
: >"$repo/$mig/037_new_on_main.sql"
git -C "$repo" add -A && git -C "$repo" commit -qm "main took 037 after the branch point"
git -C "$repo" checkout -q renamer
: >"$repo/$mig/037_mine.sql"
run "$mig" other
check 1 "a file added to the ref after the branch point still counts"
expect_out "037_mine.sql 037_new_on_main.sql"
rm "$repo/$mig/037_mine.sql"
git -C "$repo" checkout -q other
git -C "$repo" checkout -qb deleter
git -C "$repo" rm -q "$mig/035_clash.sql"
: >"$repo/$mig/035_sneaky.sql"
git -C "$repo" add -A && git -C "$repo" commit -qm "delete 035_clash, add 035_sneaky"
run "$mig" other
check 1 "deleting a base migration without renaming it does not hide a clash"
expect_out "035_clash.sql 035_sneaky.sql"
git -C "$repo" checkout -q other
git -C "$repo" checkout -qb lookalike
: >"$repo/$mig/038_track_identity.sql"
git -C "$repo" add -A && git -C "$repo" commit -qm "main has 038_track_identity"
git -C "$repo" checkout -qb lookalike-branch
git -C "$repo" rm -q "$mig/038_track_identity.sql"
: >"$repo/$mig/038_new.sql"
: >"$repo/$mig/039_other_identity.sql"
git -C "$repo" add -A && git -C "$repo" commit -qm "delete, reuse 038, add a file sharing the last word"
run "$mig" lookalike
check 1 "a file sharing only the last word of the name is not a rename"
git -C "$repo" checkout -q other

run
check 2 "no argument prints usage and exits 2"
run no/such/dir
check 2 "a missing directory exits 2"

[ $fail = 0 ] && echo "migration-prefixes.test.sh: all cases pass"
exit $fail
