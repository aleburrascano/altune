#!/usr/bin/env bash
set -euo pipefail

usage="usage: scripts/unexport-door.sh <module>   e.g. search, or shared/resilience
  unexports every root identifier of internal/<module> that lintdoors --exports reports as unused outside the module, with gopls rename (references and tests follow), and exits 1 listing any name gopls refused"

[ "$#" -eq 1 ] || { echo "$usage" >&2; exit 2; }
module=${1%/}
cd "$(dirname "$0")/.."

gopls_version=v0.20.0
gopls=(go run "golang.org/x/tools/gopls@$gopls_version")
if command -v gopls >/dev/null; then gopls=(gopls); fi

reserved=" break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var any append bool byte cap clear close comparable complex complex128 complex64 copy delete error false float32 float64 imag int int16 int32 int64 int8 iota len make max min new nil panic print println real recover rune string true uint uint16 uint32 uint64 uint8 uintptr "

lowered() {
  local name=$1
  if [[ $name =~ ^[A-Z][A-Z0-9]*$ ]]; then
    tr 'A-Z' 'a-z' <<<"$name"
  elif [[ $name =~ ^([A-Z][A-Z0-9]*)([A-Z][a-z].*)$ ]]; then
    printf '%s%s' "$(tr 'A-Z' 'a-z' <<<"${BASH_REMATCH[1]}")" "${BASH_REMATCH[2]}"
  elif [[ $name =~ ^[A-Z][A-Z0-9]*s$ ]]; then
    tr 'A-Z' 'a-z' <<<"$name"
  else
    printf '%s%s' "$(tr 'A-Z' 'a-z' <<<"${name:0:1}")" "${name:1}"
  fi
}

tool=$(mktemp)
trap 'rm -f "$tool"' EXIT
go build -o "$tool" ./scripts/lintdoors || { echo "unexport-door: lintdoors does not build" >&2; exit 2; }
modules=$("$tool" --summary . | grep -oE '^[a-z0-9/_-]+:' | tr -d ':') || true
if ! grep -qxF "$module" <<<"$modules"; then
  echo "unexport-door: $module is not a module; give one such as search or shared/resilience (modules: $(tr '\n' ' ' <<<"$modules"))" >&2
  exit 2
fi
status=0
output=$("$tool" --exports .) || status=$?
if [ "$status" -ne 0 ] && [ "$status" -ne 1 ]; then
  echo "unexport-door: lintdoors failed with exit $status; run go run ./scripts/lintdoors --exports . to see why" >&2
  exit 2
fi
listing=$(grep -F " is exported by module $module but " <<<"$output" || true)
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
  if [[ $reserved == *" $target "* ]]; then
    failed+=("$name at $position: $target is a Go keyword or predeclared name, pick a name")
    continue
  fi
  if out=$("${gopls[@]}" rename -w "$position" "$target" 2>&1); then
    renamed=$((renamed + 1))
  else
    failed+=("$name at $position: ${out//$'\n'/ }")
  fi
done <<<"$listing"

echo "unexport-door: renamed $renamed in $module, ${#failed[@]} refused"
if [ "${#failed[@]}" -gt 0 ]; then
  printf 'unexport-door: refused: %s\n' "${failed[@]}" >&2
  exit 1
fi
