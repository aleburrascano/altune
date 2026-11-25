#!/usr/bin/env bash
cd "$(dirname "$0")/../.." || exit 1
root=$PWD
fail=0

[ -d "$root/client/node_modules" ] || { echo "check-file.test.sh: client/node_modules is missing, run bash scripts/worktree-deps.sh client"; exit 1; }

scratch=$(mktemp -d "${TMPDIR:-/tmp}/check file.XXXXXX")
trap 'rm -rf "$scratch"' EXIT
clone=$scratch/repo
git clone -q --shared "$root" "$clone" || exit 1
cp "$root/scripts/check-file.sh" "$root/scripts/check-file.mjs" "$clone/scripts/"
ln -s "$root/client/node_modules" "$clone/client/node_modules"
script=$clone/scripts/check-file.sh

commit() { git -C "$clone" add -A && git -C "$clone" -c user.name=test -c user.email=test@example.com commit -q --no-verify -m "$1"; }

OUT=""; CODE=0
run() { OUT=$(bash "$script" "$@" 2>&1); CODE=$?; }
check() {
  [ "$CODE" = "$1" ] || { echo "FAIL want exit $1 got $CODE: $2"; printf '    %s\n' "$OUT"; fail=1; }
}
expect_out() {
  grep -qF -- "$1" <<<"$OUT" || { echo "FAIL $2: output lacks '$1'"; printf '    %s\n' "$OUT"; fail=1; }
}

run; check 2 "no argument is a usage error"
run "$clone/no/such/file.go"; check 2 "a missing file is a usage error"
echo x >"$scratch/outside.go"
run "$scratch/outside.go"; check 2 "a file outside the checkout is refused"
run "$root/README.md"; check 2 "a file in another checkout is refused"
run "$clone/README.md"; check 0 "a file nothing checks passes"
[ -z "$OUT" ] || { echo "FAIL a file nothing checks prints nothing"; fail=1; }

go_dir=$clone/services/go-api/internal/zzcheckfileprobe
go_file=$go_dir/probe.go
mkdir -p "$go_dir"
printf 'package zzcheckfileprobe\n\nimport "os"\n\nfunc Before() { os.Remove("before") }\n' >"$go_file"
commit "go probe with an existing finding"
printf '\nfunc Clean() int { return 1 }\n' >>"$go_file"
run "$go_file"; check 0 "a clean Go edit passes next to an existing finding"
printf '\nfunc After() { os.Remove("after") }\n' >>"$go_file"
run "$go_file"; check 1 "a new Go finding fails"
expect_out "errcheck" "the Go failure names the linter"
printf 'package zzcheckfileprobe\n\nfunc Broken() { undefinedCheckfileThing() }\n' >"$go_dir/broken.go"
run "$go_dir/broken.go"; check 1 "Go that does not compile fails"
expect_out "undefinedCheckfileThing" "the compile failure names the symbol"
rm -f "$go_dir/broken.go"

ts_dir=$clone/client/src/shared
lint_file=$ts_dir/zzCheckfileLint.ts
format_file=$ts_dir/zzCheckfileFormat.ts
printf 'const unusedBefore = 1;\nexport const kept = 1;\n' >"$lint_file"
printf 'export const   spaced = 1;\n' >"$format_file"
commit "ts probes with an existing eslint error and existing bad formatting"
printf 'export const added = 2;\n' >>"$lint_file"
run "$lint_file"; check 0 "a clean TS edit passes next to an existing eslint error"
printf 'const unusedAfter = 3;\n' >>"$lint_file"
run "$lint_file"; check 1 "a new eslint error fails"
expect_out "unusedAfter" "the eslint failure names the variable"
printf 'export const alsoAdded = 2;\n' >>"$format_file"
run "$format_file"; check 0 "an edit to a file that was already unformatted passes"

clean_file=$ts_dir/zzCheckfileClean.ts
printf 'export const tidy = 1;\n' >"$clean_file"
commit "a formatted ts file"
printf 'export const   messy = 2;\n' >>"$clean_file"
run "$clean_file"; check 1 "unformatting a formatted file fails"
expect_out "not Prettier-formatted" "the prettier failure says so"

rm "$clone/client/node_modules"
run "$clean_file"; check 3 "missing node_modules is reported as a missing tool"
expect_out "worktree-deps.sh" "the missing-tool message says how to install"

[ $fail = 0 ] && echo "check-file.test.sh: all cases pass"
exit $fail
