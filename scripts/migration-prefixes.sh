#!/usr/bin/env bash
set -uo pipefail

usage() {
  echo "usage: migration-prefixes.sh <migrations-dir> [git-ref]" >&2
  echo "fails when two .sql files in the dir, or in the dir at git-ref, share a number prefix" >&2
  exit 2
}

[ $# -ge 1 ] && [ $# -le 2 ] || usage
dir=${1%/}
ref=${2:-}
[ -d "$dir" ] || { echo "migration-prefixes: $dir: no such directory" >&2; exit 2; }

known_pair="031_acquisition_job_queue_available_at_index.sql 031_acquisition_run_attempts.sql"
known_reason="both on main since 2026-09-30; deploy/prod_host.sh records each applied migration in schema_migrations by its full file name, so renaming either would re-apply it under a new version on every database that already ran it and leave an orphan row that staging-sync.sh treats as reverted or renamed"

names() {
  local f
  for f in "$dir"/*.sql; do
    [ -f "$f" ] && basename "$f"
  done
  if [ -n "$ref" ]; then
    local base
    base=$(git merge-base HEAD "$ref" 2>/dev/null)
    git ls-tree --name-only "$ref" -- "$dir/" 2>/dev/null | grep -E '\.sql$' | while read -r f; do
      if [ ! -f "$f" ] && [ -n "$base" ] && git cat-file -e "$base:$f" 2>/dev/null &&
        name=${f##*/} && compgen -G "$dir/[0-9]*_${name#*_}" >/dev/null; then
        continue
      fi
      basename "$f"
    done
  fi
}

all=$(names | LC_ALL=C sort -u)
status=0
for prefix in $(grep -oE '^[0-9]+' <<<"$all" | LC_ALL=C sort | uniq -d); do
  group=$(grep -E "^${prefix}_" <<<"$all" | LC_ALL=C sort | tr '\n' ' ')
  group=${group% }
  if [ "$group" = "$known_pair" ]; then
    echo "migration-prefixes: allowing the known $prefix pair: $known_reason"
    continue
  fi
  echo "migration-prefixes: number $prefix is taken by more than one migration: $group"
  status=1
done

if [ $status = 1 ]; then
  echo "migration-prefixes: give the newer migration the next free number${ref:+ (the files at $ref count too, so rebase first)}"
fi
exit $status
