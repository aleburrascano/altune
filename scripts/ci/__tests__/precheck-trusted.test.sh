#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/precheck-trusted.sh
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

cat > "$work/forge" <<'STUB'
#!/usr/bin/env bash
[ -n "${FORGE_FAIL:-}" ] && exit 1
printf '%s\n' "$STATUSES"
STUB
chmod +x "$work/forge"

repo=$work/repo
git init -q -b main "$repo"
g() { git -C "$repo" -c user.name=t -c user.email=t@t "$@"; }
g commit -q --allow-empty -m base
base=$(g rev-parse HEAD)
g checkout -q -b pr
g commit -q --allow-empty -m change
head=$(g rev-parse HEAD)

status() { jq -nc --argjson id "$1" --arg c "$2" --arg who "$3" --arg s "$4" --arg d "$5" '{id:$id,context:$c,creator:{login:$who},status:$s,description:$d}'; }
green=$(status 1 factory-precheck lane-bot success "scripts/precheck.sh green in the build sandbox against $base")

t() {
  local want=$1 desc=$2
  (cd "$repo" && FORGE="$work/forge" bash "$script" "$head" main) >/dev/null 2>&1; got=$?
  [ "$got" = "$want" ] || { echo "FAIL want exit $want got $got: $desc"; fail=1; }
}

export STATUSES="[$green]" FORGE_FAIL=""
t 0 "lane-bot marked this commit green against the same base"
STATUSES="[]"
t 1 "no mark"
STATUSES="[$(status 1 factory-precheck aleburrascano success "scripts/precheck.sh green in the build sandbox against $base")]"
t 1 "a mark by anyone but lane-bot"
STATUSES="[$(status 1 factory-precheck lane-bot success "scripts/precheck.sh green in the build sandbox against 0000000")]"
t 1 "a mark against another base"
STATUSES="[$green,$(status 2 factory-precheck lane-bot failure "red")]"
t 1 "the newest mark is not success"
STATUSES="[$(status 1 factory-review lane-bot success "against $base")]"
t 1 "another context"
STATUSES="[$green]" FORGE_FAIL=1
t 1 "forge fails"

[ "$fail" = 0 ] && echo "precheck-trusted tests ok"
exit $fail
