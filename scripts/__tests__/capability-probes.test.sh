#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/capability-probes.sh
fail=0

scratch=$(mktemp -d); trap 'rm -rf "$scratch"' EXIT
repo=$scratch/repo
pkg=services/go-api/internal/x

OUT=""; CODE=0
run() { OUT=$(cd "$repo" && git add -A && bash "$script" 2>&1); CODE=$?; }
check() {
  [ "$CODE" = "$1" ] || { echo "FAIL want $1 got $CODE: $2"; sed 's/^/    /' <<<"$OUT"; fail=1; }
}
expect_out() {
  grep -qF -- "$1" <<<"$OUT" || { echo "FAIL output should mention '$1'"; sed 's/^/    /' <<<"$OUT"; fail=1; }
}

git init -q "$repo" && git -C "$repo" config user.email t@t && git -C "$repo" config user.name t
mkdir -p "$repo/$pkg"
printf 'package x\n\nfunc f(w any) bool {\n\t_, ok := w.(http.Flusher)\n\treturn ok\n}\n' >"$repo/$pkg/flush.go"
printf 'package x\n\nfunc g(ctx C) string {\n\tv, _ := ctx.Value(k{}).(ids.UserId)\n\treturn v\n}\n' >"$repo/$pkg/ctx.go"
run
check 0 "a flusher check and a context value read pass"

printf 'package x\n\nfunc h(q any) bool {\n\t_, ok := q.(ports.UpgradeJobQueue)\n\treturn ok\n}\n' >"$repo/$pkg/probe.go"
run
check 1 "a qualified port assertion fails"
expect_out "$pkg/probe.go"
rm "$repo/$pkg/probe.go"

printf 'package x\n\nfunc h(q any) bool {\n\t_, ok := q.(interface{ Counters() int })\n\treturn ok\n}\n' >"$repo/$pkg/inline.go"
run
check 1 "an inline interface assertion fails"
expect_out "$pkg/inline.go"
rm "$repo/$pkg/inline.go"

printf 'package x\n\nfunc h(q any) bool {\n\t_, ok := q.(ports.UpgradeJobQueue)\n\treturn ok\n}\n' >"$repo/$pkg/probe_test.go"
run
check 0 "a test file may assert"

[ "$fail" = 0 ] && echo "capability-probes: ok"
exit "$fail"
