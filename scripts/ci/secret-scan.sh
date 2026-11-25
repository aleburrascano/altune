#!/usr/bin/env bash
set -euo pipefail
usage="usage: secret-scan.sh BASE_REF   run in the repo; fails when a commit in BASE_REF..HEAD adds something gitleaks reads as a secret, and prints only the rule, file, line and commit, never the value"
[ $# = 1 ] || { echo "$usage" >&2; exit 2; }
base=$1
gitleaks=github.com/zricethezav/gitleaks/v8@v8.30.1
root=$(git rev-parse --show-toplevel)
git rev-parse --verify --quiet "$base^{commit}" >/dev/null || { echo "secret-scan: $base is not a commit here, so nothing was checked" >&2; exit 2; }
report=$(mktemp)
trap 'rm -f "$report"' EXIT
if ! go run "$gitleaks" git "$root" --log-opts="$base..HEAD" --config "$root/.gitleaks.toml" --redact=100 --no-banner --log-level error --exit-code 0 --report-format json --report-path "$report" >/dev/null; then
  echo "secret-scan: go run $gitleaks failed, so nothing was checked" >&2
  exit 2
fi
count=$(jq length "$report")
if [ "$count" != 0 ]; then
  echo "secret-scan: $count possible secret(s) added in $base..HEAD (values hidden):" >&2
  jq -r '.[] | "  \(.RuleID)  \(.File):\(.StartLine)  commit \(.Commit[0:10])"' "$report" >&2
  echo "secret-scan: remove the secret and rotate it, since it is in history; if it is a public key or a fake fixture, add its path to .gitleaks.toml with the reason" >&2
  exit 1
fi
echo "secret-scan: no secrets added in $base..HEAD"
