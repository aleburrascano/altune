#!/usr/bin/env bash
set -uo pipefail

readme=services/go-api/internal/library/README.md
src=services/go-api

[ -f "$readme" ] || { echo "library-table-readers: $readme: no such file" >&2; exit 2; }

tables=$(grep -m1 '^It owns the tables' "$readme" | grep -oE '`[a-z_]+`' | tr -d '`' | paste -sd'|' -)
[ -n "$tables" ] || { echo "library-table-readers: no 'It owns the tables' line in $readme" >&2; exit 2; }

listed=$(awk '/^## Who reads our tables/{on=1; next} /^## /{on=0} on' "$readme" | grep -oE '^- `[^`]+\.go`' | sed -E 's/^- `//; s/`$//' | LC_ALL=C sort -u)
[ -n "$listed" ] || { echo "library-table-readers: no readers listed under 'Who reads our tables' in $readme" >&2; exit 2; }

found=$(git grep -liE "(FROM|JOIN)[[:space:]]+(public\.)?($tables)([^a-z_]|\$)" -- "$src/*.go" ":(exclude)$src/internal/library" ":(exclude)*_test.go" | LC_ALL=C sort -u)

status=0
for f in $(comm -13 <(echo "$listed") <(echo "$found")); do
  echo "library-table-readers: $f reads a library table but is not listed under 'Who reads our tables' in $readme" >&2
  status=1
done
for f in $(comm -23 <(echo "$listed") <(echo "$found")); do
  echo "library-table-readers: $f is listed in $readme but no longer reads a library table; remove it" >&2
  status=1
done
exit $status
