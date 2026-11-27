#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/noop-reachable.sh
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

repo=$work/repo
git init -qb main "$repo"
mkdir -p "$repo/internal/ports" "$repo/internal/app"
cat >"$repo/internal/ports/noop.go" <<'EOF'
package ports

func NoopUsed() Used { return nil }

func NoopDoorOnly() DoorOnly { return nil }

func NoopTestOnly() TestOnly { return nil }

func NoopSameFileOnly() SameFile { return nil }

func wrap() SameFile { return NoopSameFileOnly() }
EOF
cat >"$repo/internal/ports/door_noop.go" <<'EOF'
package ports

var NoopDoorOnly = inner.NoopDoorOnly
EOF
cat >"$repo/internal/ports/noop_test.go" <<'EOF'
package ports_test

func TestOnly() { _ = NoopTestOnly() }
EOF
cat >"$repo/internal/app/wire.go" <<'EOF'
package app

func wire() { _ = ports.NoopUsed() }
EOF
git -C "$repo" add -A
git -C "$repo" -c user.email=t@example.com -c user.name=t commit -q -m base

run() { (cd "$repo" && bash "$script" "$@") >"$work/out.log" 2>&1; }

if run internal; then
  echo "FAIL dirty tree: exited zero"; fail=1
fi
for dead in NoopDoorOnly NoopTestOnly NoopSameFileOnly; do
  grep -q "$dead" "$work/out.log" || { echo "FAIL dirty tree: $dead was not reported"; fail=1; }
done
grep -q "NoopUsed" "$work/out.log" && { echo "FAIL dirty tree: NoopUsed is used by app/wire.go and was reported"; fail=1; }

git -C "$repo" rm -q -f internal/ports/door_noop.go internal/ports/noop_test.go
cat >"$repo/internal/ports/noop.go" <<'EOF'
package ports

func NoopUsed() Used { return nil }
EOF
git -C "$repo" add -A
run internal || { echo "FAIL clean tree: exited non-zero"; sed 's/^/    /' "$work/out.log"; fail=1; }

(cd "$repo" && bash "$script" missing >/dev/null 2>&1); [ $? -eq 2 ] || { echo "FAIL missing directory: want exit 2"; fail=1; }
(cd "$repo" && bash "$script" a b >/dev/null 2>&1); [ $? -eq 2 ] || { echo "FAIL two arguments: want exit 2"; fail=1; }

[ "$fail" = 0 ] && echo "noop-reachable: all passed"
exit "$fail"
