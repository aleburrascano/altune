#!/usr/bin/env bash
set -euo pipefail
usage="usage: go-mutate.sh BASE_REF MODULE_DIR   mutation-tests the Go lines MODULE_DIR changed since BASE_REF, package by package, and fails on any mutant a test does not catch"
[ $# = 2 ] || { echo "$usage" >&2; exit 2; }
base=$1
dir=$2
gremlins=github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0

cd "$dir"
module_prefix=$(git rev-parse --show-prefix)
mapfile -t packages < <(git -c core.quotePath=false diff --find-renames --name-only --relative --merge-base "$base" -- . | grep -E '\.go$' | grep -vE '_test\.go$' | xargs -r -n1 dirname | sort -u)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

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

mutant_source() {
  local LC_ALL=C file=$1 line=$2 column=$3 type=$4 operator= candidate
  local -a lines
  mapfile -t lines <"$file"
  local text=${lines[line - 1]}
  local rest=${text:column - 1}
  for candidate in continue break "&^=" "<<=" ">>=" "&^" "<<" ">>" "++" "--" "+=" "-=" "*=" "/=" "%=" "&=" "|=" "^=" "&&" "||" "==" "!=" ">=" "<=" "+" "-" "*" "/" "%" "&" "|" "^" ">" "<"; do
    if [[ $rest == "$candidate"* && -n ${replacement["$type $candidate"]+set} ]]; then operator=$candidate; break; fi
  done
  [ -n "$operator" ] || return 1
  lines[line - 1]=${text:0:column - 1}${replacement["$type $operator"]}${rest:${#operator}}
  printf '%s\n' "${lines[@]}"
}

rerun_mutant() {
  local file=$1 line=$2 column=$3 type=$4
  if ! mutant_source "$file" "$line" "$column" "$type" >"$work/mutant.go"; then
    echo "go-mutate: cannot rebuild the $type mutant at $dir/$file:$line:$column, so it cannot be checked" >&2
    exit 1
  fi
  jq -n --arg original "$PWD/$file" --arg mutant "$work/mutant.go" '{Replace: {($original): $mutant}}' >"$work/overlay.json"
  local package
  package=./$(dirname "$file")
  rerun_status=KILLED
  if ! go test -c -o /dev/null -overlay "$work/overlay.json" "$package" >/dev/null 2>&1; then
    rerun_status="NOT VIABLE"
  elif go test -count=1 -overlay "$work/overlay.json" -timeout 10m "$package" >/dev/null 2>&1; then
    rerun_status=LIVED
  fi
}

real_git=$(command -v git)
mkdir "$work/bin"
cat >"$work/bin/git" <<'WRAPPER'
#!/usr/bin/env bash
set -euo pipefail
[ "${1:-}" = diff ] || exec "$GO_MUTATE_REAL_GIT" "$@"
shift
"$GO_MUTATE_REAL_GIT" -c core.quotePath=false diff --find-renames --unified=0 --inter-hunk-context=0 --relative${GO_MUTATE_MODULE:+="$GO_MUTATE_MODULE"} "$@" -- . | awk -v package="$GO_MUTATE_PACKAGE" '
  function emit() {
    if (name != "" && body != "") printf "diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n%s", name, name, name, name, body
    else if (name != "") printf "diff --git a/%s b/%s\nrename from %s\nrename to %s\n", name, name, name, name
    name = ""; body = ""; in_hunks = 0
  }
  !in_hunks && /^\+\+\+ "/ { print "go-mutate: git quoted the path in \"" $0 "\", which this diff filter cannot read" > "/dev/stderr"; failed = 1; exit 1 }
  function claim(path) { if (index(path, package) == 1) name = substr(path, length(package) + 1) }
  function becomes(path) { name = ""; claim(path) }
  function same_path_header(line,   rest, path) {
    rest = substr(line, 12)
    path = substr(rest, 3, (length(rest) - 5) / 2)
    return rest == "a/" path " b/" path ? path : ""
  }
  /^diff --git / { emit(); claim(same_path_header($0)); next }
  !in_hunks && /^\+\+\+ b\// { becomes(substr($0, 7)); next }
  !in_hunks && /^--- a\// { if (name == "") claim(substr($0, 7)); next }
  !in_hunks && /^rename from / { if (name == "") claim(substr($0, 13)); next }
  !in_hunks && /^rename to / { becomes(substr($0, 11)); next }
  /^@@ / { in_hunks = 1 }
  in_hunks && name != "" { body = body $0 "\n" }
  END { if (!failed) emit() }
'
WRAPPER
chmod +x "$work/bin/git"

killed=0
survivors=()
for package in "${packages[@]}"; do
  [ -d "$package" ] || continue
  report=$work/report.json
  package_prefix=$package/
  [ "$package" != . ] || package_prefix=
  rm -f "$report"
  if ! out=$(env GOFLAGS="${GOFLAGS:+$GOFLAGS }-count=1" PATH="$work/bin:$PATH" GO_MUTATE_REAL_GIT="$real_git" GO_MUTATE_MODULE="$module_prefix" GO_MUTATE_PACKAGE="$package_prefix" go run "$gremlins" unleash --diff "$base" --output "$report" "./$package" 2>&1); then
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
  killed=$((killed + $(jq '[.files[].mutations[] | select(.status == "KILLED")] | length' "$report")))
  mapfile -t -O "${#survivors[@]}" survivors < <(jq -r --arg prefix "$dir/$package/" '.files[] | .file_name as $f | .mutations[] | select(.status == "LIVED") | "\($prefix)\($f):\(.line):\(.column) \(.status) \(.type)"' "$report")
  while read -r file line column type status; do
    rerun_mutant "$package/$file" "$line" "$column" "$type"
    case $rerun_status in
      KILLED) killed=$((killed + 1)) ;;
      LIVED) survivors+=("$dir/$package/$file:$line:$column $status $type") ;;
    esac
  done < <(jq -r '.files[] | .file_name as $f | .mutations[] | select(.status == "NOT COVERED" or .status == "TIMED OUT") | "\($f) \(.line) \(.column) \(.type) \(.status)"' "$report")
done

echo "go-mutate: $dir: ${#packages[@]} changed packages, killed $killed, survived ${#survivors[@]}"
if [ ${#survivors[@]} -gt 0 ]; then
  printf '  %s\n' "${survivors[@]}"
  echo "go-mutate: add a test that fails on each mutant above, or remove the code no test needs" >&2
  exit 1
fi
