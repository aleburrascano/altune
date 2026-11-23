#!/usr/bin/env bash
set -euo pipefail
usage="usage: go-mutate.sh BASE_REF MODULE_DIR   mutation-tests the Go lines MODULE_DIR changed since BASE_REF, package by package, and fails on any mutant a test does not catch"
[ $# = 2 ] || { echo "$usage" >&2; exit 2; }
base=$1
dir=$2
gremlins=github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0

cd "$dir"
module_prefix=$(git rev-parse --show-prefix)
mapfile -t packages < <(git diff --name-only --relative --merge-base "$base" -- . | grep -E '\.go$' | grep -vE '_test\.go$' | xargs -r -n1 dirname | sort -u)
work=$(mktemp -d)
mutated_file=
restore_mutated_file() { [ -z "$mutated_file" ] || cp "$work/original.go" "$mutated_file"; mutated_file=; }
trap 'restore_mutated_file; rm -rf "$work"' EXIT

declare -A replacement=(
  ["ARITHMETIC_BASE +"]="-" ["ARITHMETIC_BASE -"]="+" ["ARITHMETIC_BASE *"]="/" ["ARITHMETIC_BASE /"]="*" ["ARITHMETIC_BASE %"]="*"
  ["CONDITIONALS_BOUNDARY >="]=">" ["CONDITIONALS_BOUNDARY >"]=">=" ["CONDITIONALS_BOUNDARY <="]="<" ["CONDITIONALS_BOUNDARY <"]="<="
  ["CONDITIONALS_NEGATION =="]="!=" ["CONDITIONALS_NEGATION !="]="==" ["CONDITIONALS_NEGATION >="]="<" ["CONDITIONALS_NEGATION >"]="<=" ["CONDITIONALS_NEGATION <="]=">" ["CONDITIONALS_NEGATION <"]=">="
  ["INCREMENT_DECREMENT ++"]="--" ["INCREMENT_DECREMENT --"]="++"
  ["INVERT_ASSIGNMENTS +="]="-=" ["INVERT_ASSIGNMENTS -="]="+=" ["INVERT_ASSIGNMENTS *="]="/=" ["INVERT_ASSIGNMENTS /="]="*=" ["INVERT_ASSIGNMENTS %="]="%="
  ["INVERT_BITWISE &^"]="&" ["INVERT_BITWISE &"]="|" ["INVERT_BITWISE |"]="&" ["INVERT_BITWISE ^"]="&" ["INVERT_BITWISE <<"]=">>" ["INVERT_BITWISE >>"]="<<"
  ["INVERT_BWASSIGN &^="]="&=" ["INVERT_BWASSIGN &="]="|=" ["INVERT_BWASSIGN |="]="&=" ["INVERT_BWASSIGN ^="]="&=" ["INVERT_BWASSIGN <<="]=">>=" ["INVERT_BWASSIGN >>="]="<<="
  ["INVERT_LOGICAL &&"]="||" ["INVERT_LOGICAL ||"]="&&"
  ["INVERT_LOOPCTRL break"]="continue" ["INVERT_LOOPCTRL continue"]="break"
  ["INVERT_NEGATIVES -"]="+"
)
for operator in "+=" "-=" "*=" "/=" "%=" "&=" "|=" "^=" "&^=" "<<=" ">>="; do replacement["REMOVE_SELF_ASSIGNMENTS $operator"]="="; done

rerun_mutant() {
  local LC_ALL=C package=$1 file=$2 line=$3 column=$4 type=$5 operator= candidate
  local -a lines
  mapfile -t lines <"$file"
  local text=${lines[line - 1]}
  local rest=${text:column - 1}
  for candidate in continue break "&^=" "<<=" ">>=" "&^" "<<" ">>" "++" "--" "+=" "-=" "*=" "/=" "%=" "&=" "|=" "^=" "&&" "||" "==" "!=" ">=" "<=" "+" "-" "*" "/" "%" "&" "|" "^" ">" "<"; do
    if [[ $rest == "$candidate"* && -n ${replacement["$type $candidate"]+set} ]]; then operator=$candidate; break; fi
  done
  if [ -z "$operator" ]; then
    echo "go-mutate: cannot rebuild the $type mutant at $dir/$package/$file:$line:$column, so it cannot be checked" >&2
    exit 1
  fi
  lines[line - 1]=${text:0:column - 1}${replacement["$type $operator"]}${rest:${#operator}}
  cp "$file" "$work/original.go"
  mutated_file=$file
  printf '%s\n' "${lines[@]}" >"$file"
  rerun_status=KILLED
  if ! go build "./$package" >/dev/null 2>&1; then
    rerun_status="NOT VIABLE"
  elif go test -count=1 -timeout 10m "./$package" >/dev/null 2>&1; then
    rerun_status="NOT COVERED"
  fi
  restore_mutated_file
}

real_git=$(command -v git)
mkdir "$work/bin"
printf '#!/bin/sh\nif [ "$1" = diff ]; then shift; exec "%s" diff --unified=0 --inter-hunk-context=0 --relative="$GO_MUTATE_PACKAGE" "$@"; fi\nexec "%s" "$@"\n' "$real_git" "$real_git" >"$work/bin/git"
chmod +x "$work/bin/git"

killed=0
survivors=()
for package in "${packages[@]}"; do
  [ -d "$package" ] || continue
  report=$work/report.json
  rm -f "$report"
  if ! out=$(env GOFLAGS="${GOFLAGS:+$GOFLAGS }-count=1" PATH="$work/bin:$PATH" GO_MUTATE_PACKAGE="$module_prefix$package/" go run "$gremlins" unleash --diff "$base" --output "$report" "./$package" 2>&1); then
    echo "$out" >&2
    echo "go-mutate: gremlins failed in $dir/$package (a failing test or build stops it)" >&2
    exit 1
  fi
  if [ ! -f "$report" ]; then
    grep -q 'No results to report.' <<<"$out" && continue
    echo "$out" >&2
    echo "go-mutate: gremlins wrote no report for $dir/$package and did not say why" >&2
    exit 1
  fi
  killed=$((killed + $(jq '[.files[].mutations[] | select(.status == "KILLED" or .status == "TIMED OUT")] | length' "$report")))
  mapfile -t -O "${#survivors[@]}" survivors < <(jq -r --arg prefix "$dir/$package/" '.files[] | .file_name as $f | .mutations[] | select(.status == "LIVED") | "\($prefix)\($f):\(.line):\(.column) \(.status) \(.type)"' "$report")
  while read -r file line column type; do
    rerun_mutant "$package" "$package/$file" "$line" "$column" "$type"
    case $rerun_status in
      KILLED) killed=$((killed + 1)) ;;
      "NOT COVERED") survivors+=("$dir/$package/$file:$line:$column NOT COVERED $type") ;;
    esac
  done < <(jq -r '.files[] | .file_name as $f | .mutations[] | select(.status == "NOT COVERED") | "\($f) \(.line) \(.column) \(.type)"' "$report")
done

echo "go-mutate: $dir: ${#packages[@]} changed packages, killed $killed, survived ${#survivors[@]}"
if [ ${#survivors[@]} -gt 0 ]; then
  printf '  %s\n' "${survivors[@]}"
  echo "go-mutate: add a test that fails on each mutant above, or remove the code no test needs" >&2
  exit 1
fi
