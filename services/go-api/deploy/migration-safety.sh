#!/usr/bin/env bash

set -euo pipefail

MIGRATIONS_DIR="${1:-migrations}"
STATUS=0

if [ ! -d "$MIGRATIONS_DIR" ]; then
    printf 'migration-safety: %s: no such directory\n' "$MIGRATIONS_DIR" >&2
    exit 1
fi

is_contract_file() {
    grep -Eq '^--[[:space:]]*contract[[:space:]]*$' "$1"
}

report() {
    printf 'migration-safety: %s: %s needs expand-contract (mark the file -- contract once no running code needs the old shape)\n' "$1" "$2"
    STATUS=1
}

check_pattern() {
    local file=$1 stripped=$2 pattern=$3 label=$4
    grep -qiE "$pattern" <<<"$stripped" && report "$file" "$label"
    return 0
}

check_add_column_clauses() {
    local file=$1 stripped=$2 clause
    while IFS= read -r clause; do
        [ -n "$clause" ] || continue
        if grep -qi 'NOT[[:space:]]\+NULL' <<<"$clause" && ! grep -qi 'DEFAULT' <<<"$clause"; then
            report "$file" "ADD COLUMN ... NOT NULL without DEFAULT"
        fi
    done < <(grep -oiE 'ADD[[:space:]]+COLUMN[^,;]*' <<<"$stripped" || true)
}

for file in "$MIGRATIONS_DIR"/[0-9][0-9][0-9]_*.sql; do
    [ -e "$file" ] || continue
    base=$(basename "$file")
    version=${base%%_*}
    [ $((10#$version)) -ge 25 ] || continue
    is_contract_file "$file" && continue

    stripped=$(sed -E 's/--.*//' "$file")
    check_pattern "$file" "$stripped" 'DROP[[:space:]]+TABLE' 'DROP TABLE'
    check_pattern "$file" "$stripped" 'DROP[[:space:]]+COLUMN' 'DROP COLUMN'
    check_pattern "$file" "$stripped" 'RENAME' 'RENAME'
    check_pattern "$file" "$stripped" 'SET[[:space:]]+NOT[[:space:]]+NULL' 'SET NOT NULL'
    check_add_column_clauses "$file" "$stripped"
done

exit "$STATUS"
