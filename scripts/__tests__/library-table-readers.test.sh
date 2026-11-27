#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/library-table-readers.sh
fail=0

scratch=$(mktemp -d); trap 'rm -rf "$scratch"' EXIT
repo=$scratch/repo
lib=services/go-api/internal/library
other=services/go-api/internal/search/internal/adapters/persistence

OUT=""; CODE=0
run() { OUT=$(cd "$repo" && git add -A && bash "$script" 2>&1); CODE=$?; }
check() {
  [ "$CODE" = "$1" ] || { echo "FAIL want $1 got $CODE: $2"; sed 's/^/    /' <<<"$OUT"; fail=1; }
}
expect_out() {
  grep -qF -- "$1" <<<"$OUT" || { echo "FAIL output should mention '$1'"; sed 's/^/    /' <<<"$OUT"; fail=1; }
}

git init -q "$repo" && git -C "$repo" config user.email t@t && git -C "$repo" config user.name t
mkdir -p "$repo/$lib" "$repo/$other"
cat >"$repo/$lib/README.md" <<EOF
It owns the tables \`playlists\`, \`tracks\`.

## Who reads our tables

- \`$other/reader.go\` reads \`tracks.id\`.
EOF
printf 'package p\nconst q = `SELECT id FROM tracks`\n' >"$repo/$other/reader.go"
printf 'package p\nconst q = `SELECT id FROM tracks`\n' >"$repo/$lib/own.go"
printf 'package p\nconst q = `SELECT id FROM tracks_other`\n' >"$repo/$other/similar.go"

run
check 0 "a listed reader, the library itself and a similar table name pass"

printf 'package p\nconst q = `SELECT 1 FROM playlists p JOIN tracks t ON t.id = p.x`\n' >"$repo/$other/unlisted.go"
run
check 1 "an unlisted reader fails"
expect_out "$other/unlisted.go reads a library table but is not listed"
rm "$repo/$other/unlisted.go"

printf 'package p\nconst q = `SELECT id FROM tracks`\n' >"$repo/$other/reader_test.go"
run
check 0 "a test file may read the table"
rm "$repo/$other/reader_test.go"

printf 'package p\n' >"$repo/$other/reader.go"
run
check 1 "a listed file that no longer reads fails"
expect_out "no longer reads a library table"

[ "$fail" = 0 ] && echo "library-table-readers: ok"
exit "$fail"
