#!/usr/bin/env bash
eval_env_file=~/altune/services/go-api/.env.${1:-production}
eval_env_lines=$(ssh -i /home/ubuntu/.ssh/altune-prod.key -o BatchMode=yes -o ConnectTimeout=15 ubuntu@altune.duckdns.org \
  "grep -E '^(DATABASE_URL|SUPABASE_JWT_JWKS_URL|SUPABASE_PROJECT_URL|SUPABASE_JWT_AUD|LASTFM_API_KEY|DISCOGS_TOKEN|FANARTTV_API_KEY|GENIUS_ACCESS_TOKEN|MUSICBRAINZ_USER_AGENT)=' $eval_env_file") || eval_env_lines=
while IFS= read -r eval_env_line; do
  [ -n "$eval_env_line" ] || continue
  eval_env_value=${eval_env_line#*=}
  eval_env_value=${eval_env_value%\"}
  eval_env_value=${eval_env_value#\"}
  [ -n "$eval_env_value" ] || continue
  echo "::add-mask::$eval_env_value"
  export "${eval_env_line%%=*}=$eval_env_value"
done <<<"$eval_env_lines"
eval_env_missing=
for eval_env_key in DATABASE_URL SUPABASE_JWT_JWKS_URL SUPABASE_PROJECT_URL MUSICBRAINZ_USER_AGENT LASTFM_API_KEY; do
  [ -n "${!eval_env_key:-}" ] || eval_env_missing="$eval_env_missing $eval_env_key"
done
for eval_env_key in DISCOGS_TOKEN FANARTTV_API_KEY GENIUS_ACCESS_TOKEN; do
  [ -n "${!eval_env_key:-}" ] || echo "::warning::$eval_env_key is not set in $eval_env_file on the VM; that provider is left out of this run"
done
if [ -n "$eval_env_missing" ]; then
  echo "::notice::discovery eval skipped: not set in $eval_env_file on the VM:$eval_env_missing"
  return 1 2>/dev/null || exit 1
fi
