#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/test-backend-migrate.sh
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

mkdir -p "$work/scripts/ci" "$work/services/go-api/migrations" "$work/bin" "$work/elsewhere"
cp "$script" "$work/scripts/ci/"
: >"$work/services/go-api/migrations/001_a.sql"
: >"$work/services/go-api/migrations/002_b.sql"
printf '#!/usr/bin/env bash\nprintf "%%s\\n" "$*" >>"$STUB_LOG"\n' >"$work/bin/psql"
chmod +x "$work/bin/psql"

for dir in "$work" "$work/services/go-api" "$work/elsewhere"; do
  : >"$work/psql.log"
  out=$(cd "$dir" && PATH="$work/bin:$PATH" STUB_LOG="$work/psql.log" DATABASE_URL=postgres://x bash "$work/scripts/ci/test-backend-migrate.sh" 2>&1) || { echo "FAIL from ${dir#"$work"}: $out"; fail=1; continue; }
  want=$(printf '%s\n' "postgres://x -v ON_ERROR_STOP=1 -q -f migrations/001_a.sql" "postgres://x -v ON_ERROR_STOP=1 -q -f migrations/002_b.sql")
  [ "$(cat "$work/psql.log")" = "$want" ] || { echo "FAIL from ${dir#"$work"} the migrations were not applied in order: $(cat "$work/psql.log")"; fail=1; }
done

: >"$work/psql.log"
out=$(cd "$work" && CDPATH="$work" PATH="$work/bin:$PATH" STUB_LOG="$work/psql.log" DATABASE_URL=postgres://x bash scripts/ci/test-backend-migrate.sh 2>&1) || { echo "FAIL with CDPATH set: $out"; fail=1; }
[ "$(wc -l <"$work/psql.log")" = 2 ] || { echo "FAIL with CDPATH set the migrations were not applied: $(cat "$work/psql.log")"; fail=1; }

rm -rf "$work/services"
out=$(cd "$work/elsewhere" && PATH="$work/bin:$PATH" STUB_LOG="$work/psql.log" DATABASE_URL=postgres://x bash "$work/scripts/ci/test-backend-migrate.sh" 2>&1) && rc=0 || rc=$?
{ [ "$rc" = 2 ] && grep -qF "services/go-api was not found" <<<"$out"; } || { echo "FAIL a missing module folder: exit $rc, output: $out"; fail=1; }

[ "$fail" = 0 ] && echo "test-backend-migrate.test.sh: all cases pass"
exit "$fail"
