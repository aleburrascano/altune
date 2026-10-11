#!/usr/bin/env bash
set -euo pipefail
ssh_vm() { ssh -i "$VM_KEY" -o BatchMode=yes -o ConnectTimeout=15 "$VM" "$@"; }
incoming="altune-ota-incoming/$(basename "$WT")"
ssh_vm "mkdir -p ~/$incoming"
scp -i "$VM_KEY" -o BatchMode=yes -o ConnectTimeout=15 "$WT/ota-release/update.tgz" "$WT/services/go-api/deploy/ota-release.sh" "$VM:$incoming/"
ssh_vm "bash ~/$incoming/ota-release.sh ~/$incoming/update.tgz; rc=\$?; rm -rf ~/$incoming; exit \$rc"
