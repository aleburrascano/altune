#!/usr/bin/env bash
set -euo pipefail

auth_dir=$(cd "$(dirname "$0")/../supabase/auth" && pwd)
api=https://api.supabase.com/v1/projects
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

drift_program() {
  cat <<'JQ'
def sms_provider_of: sub("^sms_"; "") | sub("_(auth_token|access_key|api_key|api_secret)$"; "");

def provider_enabled($desired):
  if test("^external_.*_secret$") then $desired[sub("_secret$"; "_enabled")] == true
  elif test("^hook_.*_secrets$") then $desired[sub("_secrets$"; "_enabled")] == true
  elif . == "security_captcha_secret" then $desired.security_captcha_enabled == true
  elif . == "smtp_pass" then $desired.smtp_host != null
  elif startswith("sms_") then $desired.external_phone_enabled == true and $desired.sms_provider == sms_provider_of
  else false end;

. as $live
| $base[0] as $shared
| $shared["$secrets"] as $secrets
| ($shared + $tier[0] | del(.["$secrets"], .["$ignore"])) as $desired
| ($secrets + $shared["$ignore"] | map({(.): true}) | add) as $unmanaged
| ($desired | with_entries(select(.key as $key | $unmanaged | has($key) | not))) as $managed
| [
    ($managed | to_entries[] | . as {key: $key, value: $want}
      | if ($live | has($key) | not) then {kind: "absent", key: $key, desired: $want}
        elif $live[$key] != $want then {kind: "value", key: $key, live: $live[$key], desired: $want}
        else empty end),
    ($secrets[] | select(provider_enabled($desired) and $live[.] == null) | {kind: "unset", key: .}),
    ($live | keys_unsorted[] | . as $key | select(($desired | has($key)) or ($unmanaged | has($key)) | not) | {kind: "new", key: .})
  ]
JQ
}

drift_line_program() {
  cat <<'JQ'
.[] | if .kind == "value" then "\(.key): \(.live | tojson) → \(.desired | tojson)"
      elif .kind == "absent" then "\(.key): absent → \(.desired | tojson)"
      elif .kind == "unset" then "\(.key): unset"
      else "NEW KEY \(.key)" end
JQ
}


usage() {
  echo "usage: supabase-auth.sh diff|apply prod|staging" >&2
  exit 2
}

die() {
  echo "supabase-auth: $2" >&2
  exit "$1"
}

access_token() {
  local token_file=$HOME/.supabase/access-token token=${SUPABASE_ACCESS_TOKEN:-}
  if [ -z "$token" ] && [ -r "$token_file" ]; then
    token=$(tr -d '[:space:]' <"$token_file")
  fi
  [ -n "$token" ] || die 2 "no Supabase token: set SUPABASE_ACCESS_TOKEN or write $token_file"
  [[ $token =~ ^[A-Za-z0-9_.-]+$ ]] || die 2 "the Supabase token has characters outside [A-Za-z0-9_.-]"
  printf '%s' "$token"
}

call_auth_config() {
  local method=$1 out=$2 body=${3:-} status
  local args=(-sS --max-time 30 -K - -X "$method" -o "$out" -w '%{http_code}')
  [ -z "$body" ] || args+=(-H 'Content-Type: application/json' --data-binary "@$body")
  status=$(printf 'header = "Authorization: Bearer %s"\n' "$token" | curl "${args[@]}" "$api/$ref/config/auth") || status=000
  [[ $status == 2?? ]] && return 0
  echo "supabase-auth: $method $tier auth config failed: HTTP $status $(head -c 300 "$out" 2>/dev/null)" >&2
  return 1
}

compute_drift() {
  call_auth_config GET "$work/live.json" || exit "$1"
  jq -e 'type == "object"' "$work/live.json" >/dev/null 2>&1 || die "$1" "GET $tier auth config did not return a JSON object"
  jq --slurpfile base "$auth_dir/base.json" --slurpfile tier "$auth_dir/$tier.json" "$(drift_program)" "$work/live.json" >"$work/drift.json"
}

print_drift() {
  jq -r "$(drift_line_program)" "$work/drift.json"
}

drift_count() {
  jq "$1" "$work/drift.json"
}

run_diff() {
  compute_drift 2
  if [ "$(drift_count length)" = 0 ]; then
    echo "no drift"
    return 0
  fi
  print_drift
  return 1
}

confirmed() {
  local answer=
  printf 'Apply %d change(s) to %s? [y/N] ' "$1" "$tier" >&2
  read -r answer || true
  [ -t 0 ] || echo >&2
  [ "$answer" = y ] || [ "$answer" = Y ]
}

run_apply() {
  local changes
  compute_drift 1
  if [ "$(drift_count length)" = 0 ]; then
    echo "no drift"
    return 0
  fi
  print_drift
  changes=$(drift_count 'map(select(.kind == "value")) | length')
  if [ "$changes" = 0 ]; then
    echo "nothing apply can change on $tier: set secrets in the dashboard and add new keys to supabase/auth"
    return 0
  fi
  confirmed "$changes" || die 1 "declined; $tier unchanged"
  jq -c 'map(select(.kind == "value") | {(.key): .desired}) | add // {}' "$work/drift.json" >"$work/patch.json"
  call_auth_config PATCH "$work/patched.json" "$work/patch.json" || exit 1
  compute_drift 1
  local unapplied
  unapplied=$(drift_count 'map(select(.kind == "value")) | length')
  [ "$unapplied" = 0 ] || die 1 "$unapplied of $changes change(s) still differ on $tier after the PATCH"
  echo "applied $changes change(s) to $tier"
}

[ $# -eq 2 ] || usage
command=$1
tier=$2
case $command in
  diff | apply) ;;
  *) usage ;;
esac
case $tier in
  prod) ref=ellvexundmgvbbfqbzau ;;
  staging) ref=ijyjoyxhwmbmriwzazbx ;;
  *) usage ;;
esac
command -v curl >/dev/null || die 2 "curl is not installed"
command -v jq >/dev/null || die 2 "jq is not installed"
token=$(access_token)

if [ "$command" = apply ]; then
  run_apply
else
  run_diff
fi
