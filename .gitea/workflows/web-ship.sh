#!/usr/bin/env bash
set -euo pipefail
tier=$1
sha=$2
ssh_vm() { ssh -i "$VM_KEY" -o BatchMode=yes -o ConnectTimeout=15 "$VM" "$@"; }
incoming="altune-web-incoming/$sha-$tier"
ssh_vm "mkdir -p ~/$incoming"
scp -i "$VM_KEY" -o BatchMode=yes -o ConnectTimeout=15 "$WT/web-release/web.tgz" "$WT/web-release/web-release.sh" "$VM:$incoming/"
ssh_vm "bash ~/$incoming/web-release.sh $tier $sha ~/$incoming/web.tgz; rc=\$?; rm -rf ~/$incoming; exit \$rc"
