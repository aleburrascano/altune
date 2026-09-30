#!/usr/bin/env bash


set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
parent=$(mktemp -d)
trap 'rm -rf "$parent"' EXIT
FAILURES=0

setup_case() {
    local env_body=$1 has_file=${2:-yes}
    local stub_tracks=${STUB_TRACKS:-t} stub_healthy=${STUB_HEALTHY:-yes}
    local stub_green_healthy=${STUB_GREEN_HEALTHY:-yes} stub_green_stops=${STUB_GREEN_STOPS:-yes}
    local stub_running_blue_healthy=${STUB_RUNNING_BLUE_HEALTHY:-yes}
    local stub_tag_fails=${STUB_TAG_FAILS:-no} stub_signal=${STUB_SIGNAL_DURING_DRILL:-}
    WORK=$(mktemp -d -p "$parent")
    mkdir -p "$WORK/bin" "$WORK/api/deploy" "$WORK/api/migrations"
    cp "$HERE/lib.sh" "$HERE/staging.sh" "$HERE/compose.staging.yml" "$WORK/api/deploy/"
    : >"$WORK/api/migrations/001_baseline.sql"
    : >"$WORK/api/migrations/002_indexes.sql"
    : >"$WORK/api/migrations/016_constraint.sql"
    printf '%s\n' '-- migrate:no-transaction' 'SELECT 1;' \
        >"$WORK/api/migrations/020_marked_index.sql"
    printf '%s\n' 'CREATE INDEX CONCURRENTLY idx_x ON tracks (id);' \
        >"$WORK/api/migrations/021_unmarked_index.sql"
    printf '%s\n' '-- not built with CREATE INDEX CONCURRENTLY' 'SELECT 1;' \
        >"$WORK/api/migrations/022_prose_only.sql"

    if [ "$has_file" = yes ]; then
        printf '%s\n' "$env_body" >"$WORK/api/.env.staging"
    fi

    cat >"$WORK/bin/psql" <<EOF
#!/usr/bin/env bash
applied="$WORK/applied"; touch "\$applied"
query=""; prev=""; file=""; single=no
for a in "\$@"; do
    [ "\$prev" = "-c" ] && query="\$a"
    [ "\$prev" = "-f" ] && file="\$a"
    [ "\$a" = "--single-transaction" ] && single=yes
    prev="\$a"
done
[ -n "\$file" ] && printf '%s single-transaction=%s\n' \
    "\$(basename "\$file" .sql)" "\$single" >> "$WORK/applies.log"
case "\$query" in
    *"count(*) FROM schema_migrations"*) wc -l < "\$applied" | tr -d ' ' ;;
    *"to_regclass"*) printf '%s' '$stub_tracks' ;;
    *"SELECT 1 FROM schema_migrations WHERE version"*)
        v=\$(printf '%s' "\$query" | sed -E "s/.*version='([^']*)'.*/\1/")
        grep -qxF "\$v" "\$applied" && printf '1' ;;
    *"INSERT INTO schema_migrations"*)
        v=\$(printf '%s' "\$query" | sed -E "s/.*VALUES \('([^']*)'\).*/\1/")
        grep -qxF "\$v" "\$applied" || printf '%s\n' "\$v" >> "\$applied" ;;
esac
exit 0
EOF
    cat >"$WORK/bin/docker" <<EOF
#!/usr/bin/env bash
echo "docker \$*" >> "$WORK/actions.log"
[ "\$1" = tag ] && [ "$stub_tag_fails" = yes ] && exit 1
[ "\$1 \$2" = "exec altune-staging-go-api-green" ] && [ -n "$stub_signal" ] && kill -"$stub_signal" "\$PPID"
case "\$1 \$2" in
    "image inspect") grep -qxF "\$3" "$WORK/images"; exit ;;
    "tag "*) printf '%s\n' "\$3" >> "$WORK/images" ;;
    "exec altune-staging-go-api-green") exit $([ "$stub_green_healthy" = yes ] && echo 0 || echo 1) ;;
    "exec altune-staging-go-api-blue") exit $([ "$stub_running_blue_healthy" = yes ] && echo 0 || echo 1) ;;
esac
case " \$* " in
    *" stop go-api-green "*) exit $([ "$stub_green_stops" = yes ] && echo 0 || echo 1) ;;
esac
exit 0
EOF
    cat >"$WORK/bin/curl" <<EOF
#!/usr/bin/env bash
echo "curl \${*: -1}" >> "$WORK/actions.log"
exit $([ "$stub_healthy" = yes ] && echo 0 || echo 1)
EOF
    printf '#!/usr/bin/env bash\nexit 0\n' >"$WORK/bin/sleep"
    chmod +x "$WORK/bin"/*
    : >"$WORK/actions.log"
    : >"$WORK/applies.log"
    for tag in ${STUB_IMAGES-}; do printf 'altune-staging-go-api:%s\n' "$tag"; done >"$WORK/images"

    (cd "$WORK/api" && PATH="$WORK/bin:$PATH" STAGING_HEALTH_TIMEOUT=1 \
        bash deploy/staging.sh >"$WORK/out.log" 2>&1)
    RC=$?
    unset STUB_TRACKS STUB_HEALTHY
    unset STUB_IMAGES STUB_GREEN_HEALTHY STUB_GREEN_STOPS
    unset STUB_RUNNING_BLUE_HEALTHY
    unset STUB_TAG_FAILS STUB_SIGNAL_DURING_DRILL
}

fail() {
    printf 'FAIL: %s\n      %s\n' "$CASE" "$1"
    FAILURES=$((FAILURES + 1))
}

expect_rc() {
    [ "$RC" = "$1" ] || fail "expected exit $1, got $RC ($(tail -1 "$WORK/out.log"))"
}

expect_out() {
    grep -qF "$1" "$WORK/out.log" || fail "expected output to mention '$1'"
}

expect_action() {
    grep -qF "$1" "$WORK/actions.log" || fail "expected action '$1'"
}

expect_no_action() {
    grep -qF "$1" "$WORK/actions.log" && fail "unexpected action '$1'"
}

expect_apply() {
    grep -qxF "$1 single-transaction=$2" "$WORK/applies.log" ||
        fail "expected $1 applied with single-transaction=$2"
}

expect_action_before() {
    local first second
    first=$(grep -nF "$1" "$WORK/actions.log" | head -1 | cut -d: -f1)
    second=$(grep -nF "$2" "$WORK/actions.log" | head -1 | cut -d: -f1)
    if [ -z "$first" ] || [ -z "$second" ] || [ "$first" -ge "$second" ]; then
        fail "expected action '$1' before '$2'"
    fi
}

FULL_ENV=$'DATABASE_URL=postgres://u:p@h:5432/db\nOVERSEER_SUPABASE_URL=https://x.supabase.co\nOVERSEER_SUPABASE_ANON_KEY=sb_publishable_abc\nOVERSEER_OWNER_USER_ID=955fca87-3a19-415f-b9b8-c9b934b39524'
NO_URL_ENV=$FULL_ENV
FULL_ENV="$FULL_ENV"$'\nPUBLIC_HEALTH_URL=https://staging.example.test/health'

CASE="a missing env file fails before touching migrations or containers"
setup_case "" no
expect_rc 1
expect_out ".env.staging not found"
expect_no_action "compose -f deploy/compose.staging.yml up"

CASE="a missing required var fails loudly before any build"
setup_case $'DATABASE_URL=postgres://u:p@h:5432/db\nOVERSEER_SUPABASE_URL=https://x.supabase.co\nOVERSEER_SUPABASE_ANON_KEY=y'
expect_rc 1
expect_out "OVERSEER_OWNER_USER_ID"
expect_no_action "up"

CASE="an empty-valued required var counts as missing"
setup_case $'DATABASE_URL=\nOVERSEER_SUPABASE_URL=https://x.supabase.co\nOVERSEER_SUPABASE_ANON_KEY=y\nOVERSEER_OWNER_USER_ID=z'
expect_rc 1
expect_out "DATABASE_URL"

CASE="an already-migrated DB is adopted and no migration is re-run"
STUB_TRACKS=t setup_case "$FULL_ENV"
expect_rc 0
expect_out "adopting existing staging schema"
grep -qE "applying staging migration [0-9]" "$WORK/out.log" && fail "re-ran a migration on an already-migrated DB"
expect_action "compose -f deploy/compose.staging.yml up -d --build go-api-blue overseer redis"
expect_out "deployed staging"
expect_action "curl https://staging.example.test/health"

CASE="no staging health URL fails before migrations or any build"
STUB_TRACKS=f setup_case "$NO_URL_ENV"
expect_rc 1
expect_out "PUBLIC_HEALTH_URL is unset"
grep -q . "$WORK/applies.log" && fail "applied a migration without a health URL"
expect_no_action "up"

CASE="a blank or malformed staging health URL fails before migrations or any build"
for bad in "   " "staging.example.test/health" "https://staging.example.test/health "; do
    STUB_TRACKS=f setup_case "$NO_URL_ENV"$'\n'"PUBLIC_HEALTH_URL=$bad"
    expect_rc 1
    expect_out "PUBLIC_HEALTH_URL in"
    grep -q . "$WORK/applies.log" && fail "applied a migration with health URL '$bad'"
    expect_no_action "up"
done

CASE="a fresh DB applies every migration in order"
STUB_TRACKS=f setup_case "$FULL_ENV"
expect_rc 0
expect_out "applying staging migration 001_baseline"
expect_out "applying staging migration 016_constraint"
expect_out "deployed staging"

CASE="a no-transaction migration is applied in autocommit, a normal one is not"
STUB_TRACKS=f setup_case "$FULL_ENV"
expect_rc 0
expect_apply 001_baseline yes
expect_apply 020_marked_index no
expect_apply 021_unmarked_index no
expect_apply 022_prose_only yes
expect_out "020_marked_index is no-transaction"

CASE="an unhealthy go-api fails the staging deploy"
STUB_HEALTHY=no setup_case "$FULL_ENV"
expect_rc 1
expect_out "not healthy"

KEEP_PREVIOUS="docker tag altune-staging-go-api:blue altune-staging-go-api:green"
BUILD_BLUE="compose -f deploy/compose.staging.yml up -d --build go-api-blue"
BOOT_GREEN="compose -f deploy/compose.staging.yml up -d --no-deps --no-build go-api-green"
STOP_GREEN="compose -f deploy/compose.staging.yml stop go-api-green"

CASE="the running build is kept as the previous build before the new one is built, then booted on the new schema and stopped"
STUB_IMAGES=blue setup_case "$FULL_ENV"
expect_rc 0
expect_action_before "$KEEP_PREVIOUS" "$BUILD_BLUE"
expect_action_before "curl https://staging.example.test/health" "$BOOT_GREEN"
expect_action "docker exec altune-staging-go-api-green wget -q -O /dev/null http://127.0.0.1:8000/health"
expect_action_before "$BOOT_GREEN" "$STOP_GREEN"
expect_out "rollback drill: previous build healthy on the new schema"
expect_out "deployed staging"

CASE="a deploy after a drilled one boots the last deployed build and only then remembers the new build as the previous one"
STUB_IMAGES="blue green" setup_case "$FULL_ENV"
expect_rc 0
expect_action_before "$BUILD_BLUE" "$KEEP_PREVIOUS"
expect_action_before "$STOP_GREEN" "$KEEP_PREVIOUS"
expect_out "rollback drill: previous build healthy on the new schema"

CASE="a previous build that never gets healthy on the new schema fails the deploy, is still stopped, and stays the previous build"
STUB_IMAGES="blue green" STUB_GREEN_HEALTHY=no setup_case "$FULL_ENV"
expect_rc 1
expect_out "FAILED: rollback drill: previous build unhealthy on the new schema"
expect_action "compose -f deploy/compose.staging.yml logs --tail 80 go-api-green"
expect_action_before "$BOOT_GREEN" "$STOP_GREEN"
expect_no_action "$KEEP_PREVIOUS"
grep -qF "deployed staging" "$WORK/out.log" && fail "reported staging deployed after a failed rollback drill"

CASE="a previous build that will not stop fails the deploy"
STUB_IMAGES="blue green" STUB_GREEN_STOPS=no setup_case "$FULL_ENV"
expect_rc 1
expect_out "FAILED: rollback drill: go-api-green did not stop"
expect_no_action "$KEEP_PREVIOUS"

CASE="no previous build skips the drill, deploys, and keeps the new build for the next drill"
setup_case "$FULL_ENV"
expect_rc 0
expect_out "rollback drill skipped: no previous build"
expect_no_action "go-api-green"
expect_out "deployed staging"
expect_action_before "$BUILD_BLUE" "$KEEP_PREVIOUS"

CASE="a new build that never gets healthy is neither drilled nor kept as the previous build"
STUB_IMAGES="blue green" STUB_HEALTHY=no setup_case "$FULL_ENV"
expect_rc 1
expect_no_action "go-api-green"
expect_no_action "$KEEP_PREVIOUS"

CASE="the first drill fails on the running build it seeded and keeps that build as the previous one for the rerun"
STUB_IMAGES=blue STUB_GREEN_HEALTHY=no setup_case "$FULL_ENV"
expect_rc 1
expect_out "FAILED: rollback drill: previous build unhealthy on the new schema"
expect_action_before "$KEEP_PREVIOUS" "$BUILD_BLUE"
expect_action_before "$BOOT_GREEN" "$STOP_GREEN"
[ "$(grep -cF "$KEEP_PREVIOUS" "$WORK/actions.log")" = 1 ] ||
    fail "expected the previous build to be tagged exactly once, before the new build"

CASE="the first deploy whose new build never gets healthy keeps the running build as the previous one"
STUB_IMAGES=blue STUB_HEALTHY=no setup_case "$FULL_ENV"
expect_rc 1
expect_no_action "$BOOT_GREEN"
expect_action_before "$KEEP_PREVIOUS" "$BUILD_BLUE"
[ "$(grep -cF "$KEEP_PREVIOUS" "$WORK/actions.log")" = 1 ] ||
    fail "expected the unhealthy new build not to become the previous build"

CASE="the rollback drill never moves traffic off the serving blue"
for green_healthy in yes no; do
    STUB_IMAGES="blue green" STUB_GREEN_HEALTHY=$green_healthy setup_case "$FULL_ENV"
    expect_action "$BOOT_GREEN"
    expect_no_action "caddy"
    expect_no_action "stop go-api-blue"
    expect_no_action "rm "
    expect_no_action "up -d --build go-api-green"
done

CASE="a failed rollback drill tells the operator how to accept an intended schema break"
STUB_IMAGES="blue green" STUB_GREEN_HEALTHY=no setup_case "$FULL_ENV"
expect_rc 1
expect_out "docker tag altune-staging-go-api:blue altune-staging-go-api:green"

CASE="the running build is kept as the previous build before any migration is applied"
STUB_TRACKS=f STUB_IMAGES=blue setup_case "$FULL_ENV"
cp "$WORK/bin/psql" "$WORK/bin/psql-stub"
cat >"$WORK/bin/psql" <<EOF
#!/usr/bin/env bash
for a in "\$@"; do case "\$a" in *.sql) echo "psql apply \$(basename "\$a")" >> "$WORK/actions.log" ;; esac; done
exec "$WORK/bin/psql-stub" "\$@"
EOF
: >"$WORK/actions.log"
: >"$WORK/applied"
printf 'altune-staging-go-api:blue\n' >"$WORK/images"
(cd "$WORK/api" && PATH="$WORK/bin:$PATH" STAGING_HEALTH_TIMEOUT=1 \
    bash deploy/staging.sh >"$WORK/out.log" 2>&1)
RC=$?
expect_rc 0
expect_action "psql apply 001_baseline.sql"
expect_action_before "$KEEP_PREVIOUS" "psql apply 001_baseline.sql"

CASE="a running build that is not healthy is never kept as the previous build, so a healthy rebuild deploys"
STUB_IMAGES=blue STUB_RUNNING_BLUE_HEALTHY=no setup_case "$FULL_ENV"
expect_rc 0
expect_action "docker exec altune-staging-go-api-blue wget -q -O /dev/null http://127.0.0.1:8000/health"
expect_out "rollback drill skipped: no previous build"
expect_no_action "$BOOT_GREEN"
expect_action_before "$BUILD_BLUE" "$KEEP_PREVIOUS"
[ "$(grep -cF "$KEEP_PREVIOUS" "$WORK/actions.log")" = 1 ] ||
    fail "expected only the healthy new build to become the previous build"
expect_out "deployed staging"

CASE="a running build that cannot be kept as the previous build fails loudly before any migration or build"
STUB_TRACKS=f STUB_IMAGES=blue STUB_TAG_FAILS=yes setup_case "$FULL_ENV"
expect_rc 1
expect_out "FAILED: rollback drill: could not keep the running build"
grep -q . "$WORK/applies.log" && fail "applied a migration after the previous build could not be kept"
expect_no_action "$BUILD_BLUE"

CASE="a deployed build that cannot be remembered as the previous build fails loudly and says staging itself deployed"
STUB_IMAGES="blue green" STUB_TAG_FAILS=yes setup_case "$FULL_ENV"
expect_rc 1
expect_out "rollback drill: previous build healthy on the new schema"
expect_out "FAILED: staging deployed and the rollback drill passed, but"

CASE="a rollback drill interrupted by a signal still stops the previous build"
for signal in INT:130 TERM:143; do
    STUB_IMAGES="blue green" STUB_SIGNAL_DURING_DRILL=${signal%%:*} setup_case "$FULL_ENV"
    expect_rc "${signal##*:}"
    expect_out "FAILED: rollback drill interrupted"
    expect_action_before "$BOOT_GREEN" "$STOP_GREEN"
    expect_no_action "$KEEP_PREVIOUS"
done

CASE="a leftover previous build is killed so the drill boots it fresh on the new schema"
STUB_IMAGES="blue green" setup_case "$FULL_ENV"
expect_rc 0
expect_action_before "$BUILD_BLUE" "compose -f deploy/compose.staging.yml kill go-api-green"
expect_action_before "compose -f deploy/compose.staging.yml kill go-api-green" "$BOOT_GREEN"

CASE="a healthy previous build that will not stop reports both its health and the stop failure"
STUB_IMAGES="blue green" STUB_GREEN_STOPS=no setup_case "$FULL_ENV"
expect_rc 1
expect_out "rollback drill: previous build healthy on the new schema"
expect_out "FAILED: rollback drill: go-api-green did not stop"

CASE="a previous build that is unhealthy and will not stop reports both failures"
STUB_IMAGES="blue green" STUB_GREEN_HEALTHY=no STUB_GREEN_STOPS=no setup_case "$FULL_ENV"
expect_rc 1
expect_out "FAILED: rollback drill: previous build unhealthy on the new schema"
expect_out "FAILED: rollback drill: go-api-green did not stop"
expect_no_action "$KEEP_PREVIOUS"
grep -qF "previous build healthy" "$WORK/out.log" && fail "reported an unhealthy previous build as healthy"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%s check(s) failed\n' "$FAILURES"
    exit 1
fi

printf 'all staging deploy checks passed\n'
