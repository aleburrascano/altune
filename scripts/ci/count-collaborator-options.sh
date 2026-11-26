#!/usr/bin/env bash
set -euo pipefail
usage="usage: count-collaborator-options.sh [--check] [DIR...]
  Counts, per DIR (default: every directory under services/go-api/internal), in tracked non-test Go files:
    nil_guards    lines that compare a lower-case field to nil (x.dep == nil, x.dep != nil); a line is counted once, a field named err is ignored
    with_options  single-line With* functions whose first parameter type is not a scalar (a port, store, breaker, func or other collaborator)
  A coarse count meant for owners to drive their directories to zero: it also counts a nil check on a field that is not a collaborator
  and an option taking a named scalar type; a variadic scalar is not counted, and generic or multi-line With* signatures are not read.
  Untracked files are not read, so git add a new file first.
  DIR may be relative to the current directory. A DIR that is not a directory, or holds no tracked Go file, exits 2.
  --check exits 1 when any count is not zero."
check=0
case "${1:-}" in
  -h | --help)
    echo "$usage"
    exit 0
    ;;
  --check)
    check=1
    shift
    ;;
esac
root=$(git rev-parse --show-toplevel)
dirs=()
for arg in "$@"; do
  [ -d "$arg" ] || {
    echo "count-collaborator-options: $arg is not a directory" >&2
    exit 2
  }
  dirs+=("$(realpath --relative-to="$root" "$arg")")
done
cd "$root"
if [ "${#dirs[@]}" -eq 0 ]; then
  for d in services/go-api/internal/*/; do dirs+=("${d%/}"); done
fi
for d in "${dirs[@]}"; do
  git ls-files -- "$d/*.go" | awk '!/_test\.go$/ { found = 1 } END { exit !found }' || {
    echo "count-collaborator-options: $d holds no tracked non-test Go file" >&2
    exit 2
  }
done

tracked_matches() {
  local rc=0
  git grep -hE "$1" -- "$2/*.go" ':!*_test.go' || rc=$?
  [ "$rc" -le 1 ]
}

nil_guards() {
  tracked_matches '\b[a-z][A-Za-z0-9]*\.[a-z][A-Za-z0-9]* (==|!=) nil' "$1" |
    { grep -vE '\.err (==|!=) nil' || true; } | wc -l
}

with_options() {
  tracked_matches '^func With[A-Za-z0-9]*\(' "$1" |
    awk '
      function firstParamType(params,    depth, i, ch, entry, entries, n, parts, k, name) {
        n = 0; entry = ""; depth = 0
        for (i = 1; i <= length(params); i++) {
          ch = substr(params, i, 1)
          if (ch == "(" || ch == "[") depth++
          if (ch == ")" || ch == "]") depth--
          if (ch == "," && depth == 0) { entries[++n] = entry; entry = ""; continue }
          entry = entry ch
        }
        entries[++n] = entry
        for (k = 1; k <= n; k++) {
          gsub(/^ +| +$/, "", entries[k])
          if (index(entries[k], " ") > 0) {
            name = substr(entries[k], 1, index(entries[k], " ") - 1)
            return substr(entries[k], length(name) + 2)
          }
        }
        return ""
      }
      {
        line = $0
        sub(/^func With[A-Za-z0-9]*\(/, "", line)
        depth = 1; params = ""
        for (i = 1; i <= length(line); i++) {
          ch = substr(line, i, 1)
          if (ch == "(") depth++
          if (ch == ")") { depth--; if (depth == 0) break }
          params = params ch
        }
        if (params == "") next
        type = firstParamType(params)
        sub(/^\.\.\./, "", type)
        if (type !~ /^(int|int32|int64|uint|uint32|uint64|float32|float64|bool|string|time\.Duration)$/) count++
      }
      END { print count + 0 }'
}

total=0
printf '%-48s %10s %13s\n' DIR nil_guards with_options
for d in "${dirs[@]}"; do
  g=$(nil_guards "$d")
  o=$(with_options "$d")
  printf '%-48s %10d %13d\n' "$d" "$g" "$o"
  total=$((total + g + o))
done
printf '%-48s %24d\n' total "$total"
if [ "$check" = 1 ] && [ "$total" -ne 0 ]; then
  exit 1
fi
