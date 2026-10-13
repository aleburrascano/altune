#!/usr/bin/env bash
set -uo pipefail

if [ $# -gt 0 ]; then
  files=("$@")
else
  mapfile -t files < <(git ls-files -- '*.sh')
fi

status=0
for f in "${files[@]}"; do
  [ -f "$f" ] || continue
  grep -qE '^[[:space:]]*set +-[a-zA-Z]*o +pipefail|^[[:space:]]*set +-o +pipefail' "$f" || continue
  hits=$(grep -nE '(^|[^|])[|] *grep +-[a-zA-Z]*q' "$f")
  [ -n "$hits" ] || continue
  while IFS= read -r hit; do
    echo "pipefail-grep-q: $f:$hit"
  done <<<"$hits"
  status=1
done

if [ $status = 1 ]; then
  echo "pipefail-grep-q: grep -q exits at its first match and SIGPIPEs the writer, so a match reads as 141 under pipefail; use grep -q pattern <<<\"\$var\""
fi
exit $status
