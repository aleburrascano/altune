#!/usr/bin/env bash
set -euo pipefail
usage="usage: count-collaborator-options.sh [--check] [DIR...]
  Counts, per DIR (default: every directory under services/go-api/internal), in non-test Go files:
    nil_guards    lines that compare a lower-case field to nil (x.dep == nil, x.dep != nil), ignoring err
    with_options  With* option functions whose first parameter is not a scalar (a port, store, breaker, func or other collaborator)
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
cd "$(git rev-parse --show-toplevel)"
dirs=("$@")
if [ "${#dirs[@]}" -eq 0 ]; then
  for d in services/go-api/internal/*/; do dirs+=("${d%/}"); done
fi

nil_guards() {
  { git grep -hE '\b[a-z][A-Za-z0-9]*\.[a-z][A-Za-z0-9]* (==|!=) nil' -- "$1/**/*.go" "$1/*.go" ':!*_test.go' 2>/dev/null || true; } |
    { grep -vE '\.err (==|!=) nil' || true; } | wc -l
}

with_options() {
  { git grep -hE '^func With[A-Za-z0-9]*\([a-zA-Z0-9_]+ ' -- "$1/**/*.go" "$1/*.go" ':!*_test.go' 2>/dev/null || true; } |
    awk '{
      sub(/^func With[A-Za-z0-9]*\(/, "")
      split($0, parts, /[,)]/)
      n = split(parts[1], words, " ")
      type = words[n]
      if (type !~ /^(int|int32|int64|uint|uint32|uint64|float32|float64|bool|string|time\.Duration)$/) count++
    } END { print count + 0 }'
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
