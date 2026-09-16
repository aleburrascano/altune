#!/usr/bin/env bash

# Apply genuinely-new migrations to the PROD Supabase DB before the blue-green
# swap (task #1525 — closes the staging/prod migration-lockstep gap that let the
# two projects drift). Runs on the VM inside the human-approved deploy-prod job,
# AFTER `git reset --hard origin/main` and BEFORE blue-green.sh, so a failed
# migration aborts the deploy (set -euo pipefail via lib.sh) with the old colour
# still serving — nothing swaps onto a schema that never migrated.
#
# Shares lib.sh's migration runner with staging.sh so the two can never diverge,
# but with ONE deliberate difference: prod does NOT blindly adopt-as-baseline.
# Staging's adopt is sound because staging IS the lockstep baseline; prod is not
# guaranteed to be. A dry-run (#1525) found prod missing migrations 016/018/019/
# 020/021/022 — a blind adopt would mark those "applied" and skip them forever,
# baking in the very drift this task removes. So when the tracker is absent but the
# schema exists (the ambiguous "already-migrated-by-hand" state), prod-migrate.sh
# FAILS CLOSED and asks a human to seed the true baseline (see RUNBOOK) rather than
# guess it. Once schema_migrations reflects prod's real applied set, every future
# migration auto-applies here with no manual step.
#
# DATABASE_URL is grepped from .env.production (never `source`d — .env values can
# hold unquoted parens) and never logged.

cd "$(dirname "$0")/.." || exit
. deploy/lib.sh

ENV_FILE="${PROD_ENV_FILE:-.env.production}"
MIGRATIONS_DIR=migrations
MIGRATE_TIER=prod

if [ ! -f "$ENV_FILE" ]; then
    log "FAILED: $ENV_FILE not found; cannot apply prod migrations"
    exit 1
fi

MIGRATE_DATABASE_URL=$(read_env_var DATABASE_URL)
if [ -z "$MIGRATE_DATABASE_URL" ]; then
    log "FAILED: DATABASE_URL is unset or empty in $ENV_FILE"
    exit 1
fi

require_psql
ensure_migration_tracker

# Fail closed on the un-established baseline: schema present but nothing tracked.
# Adopting here would silently skip any migration prod never received.
if [ "$(tracked_migration_count)" = 0 ] && schema_is_present; then
    log "FAILED: prod migration baseline is not established. The schema exists but"
    log "schema_migrations is empty, and prod is NOT known to hold every migration"
    log "(a dry-run found 016/018/019/020/021/022 applied on staging but not prod)."
    log "Auto-adopting would mark those applied and skip them forever. Seed"
    log "schema_migrations to prod's REAL applied set by hand (see RUNBOOK: 'Prod"
    log "migrations') before this step can run automatically."
    exit 1
fi

log "applying prod migrations"
apply_new_migrations
log "prod migrations up to date"
