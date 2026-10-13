#!/usr/bin/env bash

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../../client"
exec node scripts/complexity-ratchet.mjs
