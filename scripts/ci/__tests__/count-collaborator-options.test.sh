#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/count-collaborator-options.sh
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

repo=$work/repo
git init -qb main "$repo"
mkdir -p "$repo/dirty/sub" "$repo/clean"
cat >"$repo/dirty/svc.go" <<'EOF'
package dirty

func (s *Service) Run() {
	if s.store == nil {
		return
	}
	if err != nil {
		return
	}
	if s.cache != nil && err == nil {
		return
	}
	if s.err != nil {
		return
	}
}

func WithStore(store ports.Store) Option                  { return nil }
func WithGrouped(a, b ports.Store) Option                 { return nil }
func WithFunc(record func(host string)) Option            { return nil }
func WithLimit(limit int) Option                          { return nil }
func WithWait(wait time.Duration) Option                  { return nil }
func WithGroupedScalars(a, b int) Option                  { return nil }
func WithoutArgs() Option                                 { return nil }
EOF
cat >"$repo/dirty/sub/deep.go" <<'EOF'
package sub

func (s *Service) Run() {
	if s.dep == nil {
		return
	}
}

func WithBreaker(breaker *Breaker) Option { return nil }
EOF
cat >"$repo/dirty/svc_test.go" <<'EOF'
package dirty

func (s *Service) Skipped() {
	if s.store == nil {
		return
	}
}

func WithTestOnly(store ports.Store) Option { return nil }
EOF
cat >"$repo/clean/ok.go" <<'EOF'
package clean

func New(store ports.Store) (*Service, error) {
	if store == nil {
		return nil, errRequired
	}
	return &Service{}, nil
}

func WithLimit(limit int) Option { return nil }
EOF
git -C "$repo" add -A
git -C "$repo" -c user.email=t@example.com -c user.name=t commit -q -m base

run() { (cd "$repo" && bash "$script" "$@") >"$work/out.log" 2>&1; }
row() { awk -v d="$1" '$1 == d { print $2, $3 }' "$work/out.log"; }

run dirty clean || { echo "FAIL report: exited non-zero without --check"; fail=1; }
[ "$(row dirty)" = "3 4" ] || { echo "FAIL dirty counts: got '$(row dirty)', want '3 4' (store, cache, sub dep; WithStore, WithGrouped, WithFunc, WithBreaker)"; fail=1; }
[ "$(row clean)" = "0 0" ] || { echo "FAIL clean counts: got '$(row clean)', want '0 0'"; fail=1; }

if run --check dirty; then
  echo "FAIL --check on a dirty directory: exited zero"; fail=1
fi
run --check clean || { echo "FAIL --check on a clean directory: exited non-zero"; sed 's/^/    /' "$work/out.log"; fail=1; }

run --check missing && { echo "FAIL --check on a missing directory: exited zero"; fail=1; }
(cd "$repo" && bash "$script" --check missing >/dev/null 2>&1); [ $? -eq 2 ] || { echo "FAIL missing directory: want exit 2"; fail=1; }

mkdir "$repo/empty"
touch "$repo/empty/notes.txt"
git -C "$repo" add -A
(cd "$repo" && bash "$script" --check empty >/dev/null 2>&1); [ $? -eq 2 ] || { echo "FAIL directory without Go files: want exit 2"; fail=1; }

(cd "$repo/clean" && bash "$script" ../dirty >"$work/out.log" 2>&1) || { echo "FAIL relative path from a subdirectory: exited non-zero"; fail=1; }
[ "$(row dirty)" = "3 4" ] || { echo "FAIL relative path from a subdirectory: got '$(row dirty)', want '3 4'"; fail=1; }

[ "$fail" = 0 ] && echo "count-collaborator-options: all passed"
exit "$fail"
