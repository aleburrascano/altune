#!/usr/bin/env bash
cd "$(dirname "$0")" || exit 1
script=$PWD/nilaway-ceiling.sh
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

printf '#!/usr/bin/env bash\nexit 0\n' > "$work/go"
cat > "$work/nilaway" <<'STUB'
#!/usr/bin/env bash
echo "${GOMEMLIMIT-unset}" >> "$MEMLOG"
for _ in $(seq "${FINDINGS:-0}"); do echo "x.go:1: Potential nil panic detected"; done
STUB
chmod +x "$work/go" "$work/nilaway"

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

[ "$fail" = 0 ] && echo "nilaway-ceiling tests ok"
[ "$fail" = 0 ]
