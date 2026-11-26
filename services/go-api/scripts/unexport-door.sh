#!/usr/bin/env bash
set -euo pipefail

usage="usage: scripts/unexport-door.sh <module>   e.g. search, or shared/backoff
  unexports every root identifier of internal/<module> that lintdoors --exports reports as unused outside the module, with gopls rename (references and tests follow), and exits 1 listing any name gopls refused"

[ "$#" -eq 1 ] || { echo "$usage" >&2; exit 2; }
module=$1
cd "$(dirname "$0")/.."

gopls_version=v0.20.0
gopls=(go run "golang.org/x/tools/gopls@$gopls_version")
if command -v gopls >/dev/null; then gopls=(gopls); fi

keywords=" break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var "

lowered() {
  local name=$1
  if [[ $name =~ ^([A-Z]+)([A-Z][a-z0-9].*)$ ]]; then
    printf '%s%s' "$(tr 'A-Z' 'a-z' <<<"${BASH_REMATCH[1]}")" "${BASH_REMATCH[2]}"
  elif [[ $name =~ ^[A-Z0-9]+$ ]]; then
    tr 'A-Z' 'a-z' <<<"$name"
  else
    printf '%s%s' "$(tr 'A-Z' 'a-z' <<<"${name:0:1}")" "${name:1}"
  fi
}

listing=$(go run ./scripts/lintdoors --exports . | grep -F " is exported by module $module but " || true)
if [ -z "$listing" ]; then
  echo "unexport-door: nothing to unexport in $module"
  exit 0
fi

renamed=0
failed=()
while IFS= read -r line; do
  position=${line%%: *}
  name=${line#*: }
  name=${name%% *}
  target=$(lowered "$name")
  if [[ $keywords == *" $target "* ]]; then
    failed+=("$name at $position: $target is a Go keyword, pick a name")
    continue
  fi
  if out=$("${gopls[@]}" rename -w "$position" "$target" 2>&1); then
    renamed=$((renamed + 1))
  else
    failed+=("$name at $position: ${out//$'\n'/ }")
  fi
done <<<"$listing"

echo "unexport-door: renamed $renamed in $module, $((${#failed[@]})) refused"
if [ "${#failed[@]}" -gt 0 ]; then
  printf 'unexport-door: refused: %s\n' "${failed[@]}" >&2
  exit 1
fi
