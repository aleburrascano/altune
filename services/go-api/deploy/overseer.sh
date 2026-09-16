#!/usr/bin/env bash

# Deploy the Overseer container to the OCI prod VM.
#
# Overseer is a single in-memory container with no go-api dependency, so it does
# not need go-api's blue-green swap (blue-green.sh only moves the go-api-* Caddy
# upstream). We rebuild the image and recreate the one container: a brief
# /overseer restart blip is acceptable and go-api traffic is untouched.
#
# The env check runs BEFORE the container is touched: a missing required
# OVERSEER_* var fails the deploy loudly here, instead of letting the new binary
# crash-loop in prod (config.validate() fails closed on the same three vars).

cd "$(dirname "$0")/.."
. deploy/lib.sh

ENV_FILE="${OVERSEER_ENV_FILE:-.env.production}"
REQUIRED_VARS="OVERSEER_OWNER_USER_ID OVERSEER_SUPABASE_URL OVERSEER_SUPABASE_ANON_KEY"

require_overseer_env() {
    if [ ! -f "$ENV_FILE" ]; then
        log "FAILED: $ENV_FILE not found; cannot deploy overseer without its env"
        exit 1
    fi
    local missing="" var
    for var in $REQUIRED_VARS; do
        # Present == a line "VAR=" with at least one non-space char of value.
        if ! grep -Eq "^[[:space:]]*${var}=.*[^[:space:]]" "$ENV_FILE"; then
            missing="$missing $var"
        fi
    done
    if [ -n "$missing" ]; then
        log "FAILED: $ENV_FILE is missing required overseer var(s):$missing"
        log "set them in $ENV_FILE before redeploying; overseer crash-loops without them"
        exit 1
    fi
}

require_overseer_env

log "building overseer"
compose build overseer

log "starting overseer"
compose up -d overseer

log "deployed overseer"
compose ps overseer
