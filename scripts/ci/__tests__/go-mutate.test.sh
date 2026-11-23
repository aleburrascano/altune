#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/go-mutate.sh
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

repo=$work/repo
mod=$repo/svc
mkdir -p "$mod/clamp" "$mod/senior" "$mod/label"
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
cat >"$mod/label/label.go" <<'GO'
package label

func Label(n int) string {
	prefix := "n"
	if n > 100 {
		return "big"
	}
	suffix := "!"
	return prefix + suffix
}
GO
cat >"$mod/label/label_test.go" <<'GO'
package label_test

import (
	"testing"

	"example.com/svc/label"
)

func TestLabel(t *testing.T) {
	if got := label.Label(5); got != "n!" {
		t.Fatalf("Label(5) = %q", got)
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

sed -i 's/18: true/18: true, 40: true/' "$mod/clamp/clamp_test.go"
commit tests-only
expect "a change to tests only has nothing to mutate" 0 '0 changed packages, killed 0, survived 0'

sed -i 's/age >= 18/18 <= age/' "$mod/clamp/clamp.go"
commit tested
expect "a changed line the tests cover passes" 0 '1 changed packages, killed [1-9][0-9]*, survived 0'

sed -i -e 's/prefix := "n"/prefix := "m"/' -e 's/suffix := "!"/suffix := "?"/' "$mod/label/label.go"
sed -i 's/"n!"/"m?"/' "$mod/label/label_test.go"
commit two-lines-apart
expect "an unchanged line between two changed lines is not mutated" 0 '2 changed packages, killed [1-9][0-9]*, survived 0'

sed -i 's/suffix := "?"/suffix := map[bool]string{true: "?", false: "!"}[n > 3]/' "$mod/label/label.go"
commit later-line-mutable
expect "the later of two changed lines in one hunk is mutated" 1 'svc/label/label.go:8:[0-9]+ LIVED CONDITIONALS_BOUNDARY'
if grep -q 'svc/label/label.go:5:' <<<"$out"; then
  echo "FAIL  the unchanged line between them stays unmutated:"; echo "$out" | sed 's/^/      /'; fail=1
else
  echo "ok    the unchanged line between them stays unmutated"
fi

cat >"$mod/senior/senior.go" <<'GO'
package senior

func Senior(age int) bool {
	return age > 64
}
GO
cat >"$mod/senior/senior_test.go" <<'GO'
package senior_test

import (
	"testing"

	"example.com/svc/senior"
)

func TestSenior(t *testing.T) {
	for age, want := range map[int]bool{30: false, 70: true} {
		if got := senior.Senior(age); got != want {
			t.Fatalf("Senior(%d) = %v, want %v", age, got, want)
		}
	}
}
GO
commit weak-test
(cd "$repo" && bash "$script" "$base" svc >/dev/null 2>&1)
expect "a mutant the tests miss fails, even with a warm test cache" 1 'svc/senior/senior.go:[0-9]+:[0-9]+ LIVED CONDITIONALS_BOUNDARY'

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

shared_base=$base
base=$(git -C "$repo" rev-parse HEAD)
mkdir -p "$mod/quota"
cat >"$mod/quota/quota.go" <<'GO'
package quota

const limit = 6 * 3

func Over(n int) bool {
	return n > limit
}
GO
cat >"$mod/quota/quota_test.go" <<'GO'
package quota_test

import (
	"testing"

	"example.com/svc/quota"
)

func TestOver(t *testing.T) {
	for n, want := range map[int]bool{18: false, 19: true} {
		if got := quota.Over(n); got != want {
			t.Fatalf("Over(%d) = %v, want %v", n, got, want)
		}
	}
}
GO
commit const-pinned
expect "a const the tests pin counts as killed, though gremlins has no coverage for const lines" 0 'killed [1-9][0-9]*, survived 0'
if [ -n "$(git -C "$repo" status --porcelain)" ]; then
  echo "FAIL  the rerun leaves the source as it was:"; git -C "$repo" status --porcelain | sed 's/^/      /'; fail=1
else
  echo "ok    the rerun leaves the source as it was"
fi

sed -i 's/18: false, 19: true/1: false, 100: true/' "$mod/quota/quota_test.go"
sed -i 's/6 \* 3/6 * 4/' "$mod/quota/quota.go"
commit const-unpinned
expect "a const no test pins fails and names the line" 1 'svc/quota/quota.go:3:[0-9]+ NOT COVERED ARITHMETIC_BASE'
cat >>"$mod/quota/quota.go" <<'GO'

const Greeting = "hel" + "lo"
GO
cat >>"$mod/quota/quota_test.go" <<'GO'

func TestGreeting(t *testing.T) {
	if quota.Greeting != "hello" {
		t.Fatal(quota.Greeting)
	}
}
GO
sed -i 's/1: false, 100: true/24: false, 25: true/' "$mod/quota/quota_test.go"
commit const-not-viable
expect "a const mutant that does not compile counts as neither killed nor survived" 0 'killed 3, survived 0'

base=$shared_base

sed -i 's/17: false/17: true/' "$mod/clamp/clamp_test.go"
commit broken
expect "a failing test stops the run loudly" 1 'gremlins failed in svc/clamp'

exit $fail
