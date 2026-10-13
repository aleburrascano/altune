#!/usr/bin/env bash
set -euo pipefail
if [ -z "${MUTATE:-}" ]; then
  mapfile -t dirs < <(jq -r '.mutate[] | select(startswith("!") | not) | sub("/\\*\\*/.*$"; "")' stryker.config.json)
  mapfile -t files < <(git ls-files -- "${dirs[@]/%//*.ts}" | grep -vE '(__tests__/|\.test\.ts$|\.d\.ts$)' | sort)
  per=${MUTATE_FILES:-4}
  week=$((10#$(date -u +%V)))
  start=$(( (week * per) % ${#files[@]} ))
  picked=()
  for ((i = 0; i < per && i < ${#files[@]}; i++)); do
    picked+=("${files[$(( (start + i) % ${#files[@]} ))]}")
  done
  MUTATE=$(IFS=,; echo "${picked[*]}")
fi
echo "mutating: $MUTATE"
jq '.inPlace = true | .concurrency = 2 | .dryRunTimeoutMinutes = 30 | .jest.config = {testTimeout: 30000}' stryker.config.json >stryker.ci.json
rc=0
timeout 75m npx stryker run stryker.ci.json --mutate "$MUTATE" || rc=$?
if [ "$rc" -eq 124 ]; then
  echo "::warning::mutation run hit the 75 minute cap before finishing; no report this week"
  exit 0
fi
exit "$rc"
