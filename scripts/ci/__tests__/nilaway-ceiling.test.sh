#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/nilaway-ceiling.sh
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

cat > "$work/go" <<'STUB'
#!/usr/bin/env bash
[ "$1" = list ] && printf 'altune/overseer\naltune/overseer/web/node_modules/flatted/golang/pkg/flatted\n'
exit 0
STUB
cat > "$work/nilaway" <<'STUB'
#!/usr/bin/env bash
echo "${GOMEMLIMIT-unset}" >> "$MEMLOG"
echo "$*" >> "$MEMLOG.args"
for _ in $(seq "${FINDINGS:-0}"); do echo "x.go:1: Potential nil panic detected"; done
STUB
chmod +x "$work/go" "$work/nilaway"
export NILAWAY_BASE_REF=none/none

run() {
  : > "$work/mem.log"
  out=$(PATH="$work:$PATH" MEMLOG="$work/mem.log" bash "$script" "$@" 2>&1); got=$?
}

check() {
  local desc=$1 want=$2 have=$3
  [ "$want" = "$have" ] || { echo "FAIL $desc: want '$want' got '$have'"; fail=1; }
}

unset GOMEMLIMIT
run
check "default limit" "4GiB" "$(cat "$work/mem.log")"

GOMEMLIMIT=2GiB run
check "caller limit kept" "2GiB" "$(cat "$work/mem.log")"

export FINDINGS=2
echo 1 > "$work/base"
run "$work/base"
check "count above ceiling exits 1" "1" "$got"
grep -q '^::error::' <<<"$out" || { echo "FAIL no ::error:: line"; fail=1; }
echo 2 > "$work/base"
run "$work/base"
check "count at ceiling exits 0" "0" "$got"

: > "$work/mem.log.args"
run
check "node_modules packages stay out of scope" "altune/overseer" "$(cat "$work/mem.log.args")"

mkdir "$work/diffbin"
cat > "$work/diffbin/nilaway" <<'STUB'
#!/usr/bin/env bash
[ -f canned.txt ] || exit 3
sed "s#@PWD@#$PWD#g" canned.txt
STUB
cp "$work/go" "$work/diffbin/go"
chmod +x "$work/diffbin/nilaway"

finding() {
  printf '@PWD@/%s:%s:2: \033[31mPotential nil panic detected\033[0m. Observed nil flow from source to dereference point:\n\t- @PWD@/%s:%s:9: %s\n' "$1" "$2" "$1" "$2" "$3"
}

repo=$work/repo
mkdir -p "$repo/services/go-api/internal"
git -C "$repo" init -q -b main
git -C "$repo" config user.email t@t
git -C "$repo" config user.name t
touch "$repo/services/go-api/internal/a.go" "$repo/services/go-api/internal/b.go"
finding internal/a.go 10 "field x of a.T flows into a deref" > "$repo/services/go-api/canned.txt"
git -C "$repo" add -A
git -C "$repo" commit -q -m base
git -C "$repo" checkout -q -b pr
{
  finding internal/a.go 14 "field x of a.T flows into a deref"
  finding internal/b.go 7 "return of b.Find flows into a deref"
} > "$repo/services/go-api/canned.txt"
echo 'package b' > "$repo/services/go-api/internal/b.go"
git -C "$repo" add -A
git -C "$repo" commit -q -m head

run_diff() {
  out=$(cd "$repo/services/go-api" && PATH="$work/diffbin:$PATH" NILAWAY_BASE_REF=main bash "$script" "$work/base" 2>&1); got=$?
}

echo 1 > "$work/base"
run_diff
check "rise against merge base exits 1" "1" "$got"
grep -q '^::group::nilaway findings$' <<<"$out" || { echo "FAIL no findings group"; fail=1; }
grep -q '^::endgroup::$' <<<"$out" || { echo "FAIL group not closed"; fail=1; }
grep -q $'\033' <<<"$out" && { echo "FAIL ansi left in log"; fail=1; }
new=${out#*New nilaway findings}
grep -q 'return of b.Find flows into a deref' <<<"$new" || { echo "FAIL new flow text missing"; fail=1; }
grep -q '^::error file=services/go-api/internal/b.go,line=7::internal/b.go:7:2: Potential nil panic detected' <<<"$new" || { echo "FAIL annotation wrong"; fail=1; }
grep -c '^::error file=' <<<"$new" | grep -qx 1 || { echo "FAIL old finding annotated as new"; fail=1; }

repo=$work/repo-no-base-run
mkdir -p "$repo/services/go-api/internal"
git -C "$repo" init -q -b main
git -C "$repo" config user.email t@t
git -C "$repo" config user.name t
touch "$repo/services/go-api/internal/a.go" "$repo/services/go-api/internal/b.go"
git -C "$repo" add -A
git -C "$repo" commit -q -m base
git -C "$repo" checkout -q -b pr
{
  finding internal/a.go 14 "field x of a.T flows into a deref"
  finding internal/b.go 7 "return of b.Find flows into a deref"
} > "$repo/services/go-api/canned.txt"
echo 'package b' > "$repo/services/go-api/internal/b.go"
git -C "$repo" add -A
git -C "$repo" commit -q -m head
run_diff
check "failed merge-base run still exits 1" "1" "$got"
grep -q 'nilaway failed on merge base' <<<"$out" || { echo "FAIL no fallback warning"; fail=1; }
grep -q '^::error file=services/go-api/internal/b.go,line=7::' <<<"$out" || { echo "FAIL fallback lacks changed-file finding"; fail=1; }
grep -q '^::error file=services/go-api/internal/a.go' <<<"$out" && { echo "FAIL fallback annotated unchanged file"; fail=1; }

repo=$work/repo-stale-ceiling
mkdir -p "$repo/services/go-api/internal"
git -C "$repo" init -q -b main
git -C "$repo" config user.email t@t
git -C "$repo" config user.name t
touch "$repo/services/go-api/internal/a.go"
{
  finding internal/a.go 10 "field x of a.T flows into a deref"
  finding internal/a.go 20 "field y of a.T flows into a deref"
} > "$repo/services/go-api/canned.txt"
git -C "$repo" add -A
git -C "$repo" commit -q -m base
git -C "$repo" checkout -q -b pr
{
  finding internal/a.go 12 "field x of a.T flows into a deref"
  finding internal/a.go 22 "field y of a.T flows into a deref"
} > "$repo/services/go-api/canned.txt"
git -C "$repo" commit -q -am head
echo 1 > "$work/base"
run_diff
check "stale ceiling still exits 1" "1" "$got"
grep -q '^::warning::no new nilaway findings versus merge base .*ceiling is stale: base has 2 findings' <<<"$out" || { echo "FAIL stale ceiling warning missing"; fail=1; }
grep -q '^New nilaway findings' <<<"$out" && { echo "FAIL empty new list printed"; fail=1; }

guardrails=$PWD/../../services/go-api/scripts/guardrails.sh
: > "$work/mem.log"
(PATH="$work:$PATH" MEMLOG="$work/mem.log" bash "$guardrails" nilaway > "$work/guardrails.out" 2>&1)
check "guardrails skips nilaway without NILAWAY_LOCAL" "" "$(cat "$work/mem.log")"
grep -q 'SKIP nilaway' "$work/guardrails.out" || { echo "FAIL guardrails does not say it skipped"; fail=1; }

[ "$fail" = 0 ] && echo "nilaway-ceiling tests ok"
[ "$fail" = 0 ]
