#!/usr/bin/env bash
set -euo pipefail
usage="usage: secret-scan.sh BASE_REF | --pending   run in the repo; fails when a commit in BASE_REF..HEAD (or the uncommitted changes against HEAD) adds something gitleaks reads as a secret, and prints only the rule, file, line and commit, never the value"
[ $# = 1 ] || { echo "$usage" >&2; exit 2; }
gitleaks=github.com/zricethezav/gitleaks/v8@v8.30.1
root=$(git rev-parse --show-toplevel)
if [ "$1" = --pending ]; then
  scope=(--pre-commit)
  label="the uncommitted changes"
else
  base=$1
  git rev-parse --verify --quiet "$base^{commit}" >/dev/null || { echo "secret-scan: $base is not a commit here, so nothing was checked" >&2; exit 2; }
  scope=(--log-opts="$base..HEAD")
  label="$base..HEAD"
fi
report=$(mktemp)
trap 'rm -f "$report"' EXIT
if ! go run "$gitleaks" git "$root" "${scope[@]}" --config "$root/.gitleaks.toml" --redact=100 --no-banner --log-level error --exit-code 0 --report-format json --report-path "$report" >/dev/null; then
  echo "secret-scan: go run $gitleaks failed, so nothing was checked" >&2
  exit 2
fi
count=$(jq length "$report")
if [ "$count" != 0 ]; then
  echo "secret-scan: $count possible secret(s) added in $label (values hidden):" >&2
  jq -r '.[] | "  \(.RuleID)  \(.File):\(.StartLine)" + (if (.Commit // "") == "" then "" else "  commit \(.Commit[0:10])" end)' "$report" >&2
  echo "secret-scan: remove the secret (rotate it if it was ever committed or pushed); if it is a public key or a fake fixture, add its path to .gitleaks.toml with the reason" >&2
  exit 1
fi
echo "secret-scan: no secrets added in $label"
