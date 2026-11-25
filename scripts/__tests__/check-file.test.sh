#!/usr/bin/env bash
cd "$(dirname "$0")/../.." || exit 1
root=$PWD
script=$root/scripts/check-file.sh
fail=0

go_pkg=services/go-api/internal/acquisition
ts_dir=client/src/shared
go_probe=$root/$go_pkg/zz_checkfile_probe.go
ts_probe=$root/$ts_dir/zzCheckfileProbe.ts
scratch=$(mktemp -d)
trap 'rm -rf "$scratch" "$go_probe" "$ts_probe"' EXIT

OUT=""; CODE=0
run() { OUT=$(bash "$script" "$@" 2>&1); CODE=$?; }
check() {
  [ "$CODE" = "$1" ] || { echo "FAIL want exit $1 got $CODE: $2"; sed 's/^/    /' <<<"$OUT"; fail=1; }
}
expect_out() {
  grep -qF -- "$1" <<<"$OUT" || { echo "FAIL $2: output lacks '$1'"; sed 's/^/    /' <<<"$OUT"; fail=1; }
}

run; check 2 "no argument is a usage error"
run "$root/no/such/file.go"; check 2 "a missing file is a usage error"
echo x >"$scratch/outside.go"
run "$scratch/outside.go"; check 2 "a file outside the checkout is refused"
run "$root/README.md"; check 0 "a file nothing checks passes silently"
[ -z "$OUT" ] || { echo "FAIL a file nothing checks prints nothing"; fail=1; }

pkg_name=$(sed -n 's/^package //p' "$(ls "$root/$go_pkg"/*.go | grep -v _test | head -1)" | head -1)
printf 'package %s\n\nfunc checkfileProbe() int { return 1 }\n\nvar _ = checkfileProbe\n' "$pkg_name" >"$go_probe"
run "$go_probe"; check 0 "clean new Go code passes"
printf 'package %s\n\nfunc checkfileProbe() { undefinedCheckfileThing() }\n' "$pkg_name" >"$go_probe"
run "$go_probe"; check 1 "broken new Go code fails"
expect_out "undefinedCheckfileThing" "the Go failure names the broken symbol"
rm -f "$go_probe"

printf 'export const checkfileProbe = 1;\n' >"$ts_probe"
run "$ts_probe"; check 0 "clean new TypeScript passes"
printf 'const checkfileUnused = 1;\nexport const checkfileProbe = 2;\n' >"$ts_probe"
run "$ts_probe"; check 1 "an unused TypeScript variable fails"
expect_out "checkfileUnused" "the TypeScript failure names the unused variable"

[ $fail = 0 ] && echo "check-file.test.sh: all cases pass"
exit $fail
