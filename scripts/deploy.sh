#!/usr/bin/env bash
# Prod deploy for the go-api. Run on the VM: bash scripts/deploy.sh
#
# Pulls main, rebuilds, and restarts — removing any container a previously
# interrupted `up` left renamed as "<hash>_altune-go-api", which otherwise
# fails the next recreate with a name conflict (the recurring deploy snag).
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo"
git pull --ff-only origin main

cd services/go-api
docker compose -f docker-compose.prod.yml build
# Clear every altune-go-api* container (running or the leftover rename) so the
# name is free before recreate.
docker ps -aq --filter 'name=altune-go-api' | xargs -r docker rm -f
docker compose -f docker-compose.prod.yml up -d --remove-orphans
docker compose -f docker-compose.prod.yml ps
