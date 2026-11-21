#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/go-mutate.sh
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

repo=$work/repo
mod=$repo/svc
mkdir -p "$mod/clamp"
git init -qb main "$repo"
commit() { git -C "$repo" add -A && git -C "$repo" -c user.email=t@example.com -c user.name=t commit -qm "$1"; }

printf 'module example.com/svc\n\ngo 1.22\n' >"$mod/go.mod"
cat >"$mod/clamp/clamp.go" <<'GO'
package clamp

func Adult(age int) bool {
	return age >= 18
}
GO
cat >"$mod/clamp/clamp_test.go" <<'GO'
package clamp_test

import (
	"testing"

	"example.com/svc/clamp"
)

func TestAdult(t *testing.T) {
	for age, want := range map[int]bool{17: false, 18: true} {
		if got := clamp.Adult(age); got != want {
			t.Fatalf("Adult(%d) = %v, want %v", age, got, want)
		}
	}
}
GO
commit base
base=$(git -C "$repo" rev-parse HEAD)

expect() {
  local name=$1 want=$2 pattern=$3 rc
  out=$(cd "$repo" && bash "$script" "$base" svc 2>&1); rc=$?
  if [ "$rc" != "$want" ] || ! grep -qE "$pattern" <<<"$out"; then
    echo "FAIL  $name: exit $rc (want $want), output:"; echo "$out" | sed 's/^/      /'; fail=1
  else
    echo "ok    $name"
  fi
}

out=$(bash "$script" 2>&1); [ $? = 2 ] && grep -q usage <<<"$out" && echo "ok    no arguments prints usage" || { echo "FAIL  no arguments"; fail=1; }

sed -i 's/age >= 18/18 <= age/' "$mod/clamp/clamp.go"
commit tested
expect "a changed line the tests cover passes" 0 'survived 0'

cat >>"$mod/clamp/clamp.go" <<'GO'

func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
GO
commit untested
expect "a changed line no test covers fails and names the line" 1 'svc/clamp/clamp.go:[0-9]+:[0-9]+ NOT COVERED'

sed -i 's/17: false/17: true/' "$mod/clamp/clamp_test.go"
commit broken
expect "a failing test stops the run loudly" 1 'gremlins failed'

exit $fail
