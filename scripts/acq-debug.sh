#!/usr/bin/env bash
# Debug audio acquisition against the live backend from a terminal, without the
# app. Everything runs on the OCI VM over SSH and is read-only: the database
# session is forced read-only, yt-dlp runs with --simulate against a throwaway
# copy of the cookie file, and no secret is ever printed.
#
# Usage: bash scripts/acq-debug.sh [--staging] <command> [args]
#   summary [days]        status mix, failure codes, which sources delivered, stuck pending (default 14 days)
#   track <text|uuid>     a track's acquisition row(s), then its log lines from the running go-api
#   client [since] [track]  acquisition_ui/client_error events from discovery_events (default
#                         since 24 hours ago; track filters on the event's track_id)
#   probe <query>         run the app's own yt-dlp search for <query>, then try extracting each
#                         candidate the way the download step would — shows bot-checks, previews, dead ids
#   tools                 binary versions and a live canary per source (is YouTube / SoundCloud reachable?)
#   logs [since] [regex]  acquisition log lines from the running go-api (default since 1h)
#   sql <select>          an ad-hoc read-only query against the tier's database
#
# Env: ALTUNE_SSH_KEY (default ~/.ssh/altune-prod.key), ALTUNE_HOST (default ubuntu@altune.duckdns.org).
# Exit: 0 ok, 1 a check failed, 3 could not run (no SSH, no container, bad usage).
set -uo pipefail

key=${ALTUNE_SSH_KEY:-$HOME/.ssh/altune-prod.key}
host=${ALTUNE_HOST:-ubuntu@altune.duckdns.org}
tier=prod
if [ "${1:-}" = --staging ]; then tier=staging; shift; fi
if [ $# -eq 0 ] || [ "$1" = help ] || [ "$1" = -h ]; then
  sed -n '2,/^set -uo/p' "$0" | sed '$d; s/^# \{0,1\}//'
  exit 0
fi

# printf %q keeps multi-word args (a probe query) intact through the remote shell.
exec ssh -i "$key" -o BatchMode=yes -o ConnectTimeout=10 "$host" \
  "bash -s -- $(printf '%q ' "$tier" "$@")" <<'REMOTE'
set -uo pipefail
tier=$1; cmd=$2; shift 2
cd /home/ubuntu/altune/services/go-api || { echo "acq-debug: no checkout on the VM"; exit 3; }

case $tier in
  prod)    prefix=altune-go-api;         envfile=.env.production ;;
  staging) prefix=altune-staging-go-api; envfile=.env.staging ;;
esac
api=$(docker ps --filter "name=^${prefix}-" --format '{{.Names}}' | head -1)

need_api() { [ -n "$api" ] || { echo "acq-debug: no running ${prefix}-* container"; exit 3; }; }

# Read-only by construction: the SET runs first in the same session, so any write
# below it fails instead of landing. The Supabase pooler drops PGOPTIONS, so the
# guard has to live in the session; `sql` below keeps callers from undoing it.
db() {
  local url
  url=$(grep -E '^DATABASE_URL=' "$envfile" | head -1 | cut -d= -f2- | tr -d "\"'")
  [ -n "$url" ] || { echo "acq-debug: no DATABASE_URL in $envfile"; exit 3; }
  { echo "SET default_transaction_read_only = on; SET statement_timeout = '30s';"; cat; } |
    psql "$url" -X -q -v ON_ERROR_STOP=1 "$@"
}

# The flags the app's searcher and downloader put in front of every yt-dlp call
# (adapters/ytdlp/searcher.go), with a scratch copy of the cookie jar because
# yt-dlp writes cookies back and the live file belongs to the app.
YTDLP_PRELUDE='ck=$(mktemp); trap "rm -f $ck" EXIT
[ -n "${YTDLP_COOKIE_FILE:-}" ] && [ -f "$YTDLP_COOKIE_FILE" ] && cp "$YTDLP_COOKIE_FILE" "$ck"
yt() { set -- --no-warnings "$@"
  [ -n "${YTDLP_JS_RUNTIME:-}" ] && set -- --js-runtimes "$YTDLP_JS_RUNTIME" --remote-components ejs:github "$@"
  [ -s "$ck" ] && set -- --cookies "$ck" "$@"
  timeout 60 yt-dlp "$@"; }'

in_api() { docker exec "$api" sh -c "$YTDLP_PRELUDE
$1" sh "${@:2}"; }

case $cmd in
summary)
  days=${1:-14}
  db -v days="$days" <<'SQL'
\echo '== status of tracks added in the window'
SELECT acquisition_status, count(*) FROM tracks
 WHERE added_at > now() - (:'days' || ' days')::interval GROUP BY 1 ORDER BY 2 DESC;
\echo '== failure codes'
SELECT split_part(failure_reason, ': ', 1) AS code, count(*) FROM tracks
 WHERE acquisition_status = 'failed' AND added_at > now() - (:'days' || ' days')::interval
 GROUP BY 1 ORDER BY 2 DESC;
\echo '== which source delivered the ready tracks, by day'
SELECT added_at::date AS day, substring(audio_source_url from '//([^/]+)/') AS source,
       acquisition_provenance AS provenance, count(*)
  FROM tracks WHERE acquisition_status = 'ready' AND added_at > now() - (:'days' || ' days')::interval
 GROUP BY 1, 2, 3 ORDER BY 1 DESC, 4 DESC;
\echo '== pending for over 30 minutes (the sweep fails these after 15m of in-flight time)'
SELECT id, left(title, 40) AS title, left(artist, 25) AS artist, added_at::timestamp(0),
       acquisition_started_at::timestamp(0) AS started_at
  FROM tracks WHERE acquisition_status = 'pending'
   AND coalesce(acquisition_started_at, added_at) < now() - interval '30 minutes'
   AND id::text NOT LIKE '11111111-1111-1111-1111-%'  -- the seed fixtures, never acquired
 ORDER BY added_at DESC LIMIT 20;
\echo '== latest failures'
SELECT left(title, 35) AS title, left(artist, 20) AS artist, added_at::timestamp(0),
       left(failure_reason, 150) AS reason
  FROM tracks WHERE acquisition_status = 'failed' ORDER BY added_at DESC LIMIT 15;
SQL
  ;;

track)
  [ $# -ge 1 ] || { echo "usage: track <text|uuid>"; exit 3; }
  ids=$(db -A -t -v q="$1" <<'SQL' | grep -E '^[0-9a-f-]{36}$'
SELECT id FROM tracks WHERE id::text = :'q' OR title ILIKE '%' || :'q' || '%' OR artist ILIKE '%' || :'q' || '%'
 ORDER BY added_at DESC LIMIT 5;
SQL
)
  [ -n "$ids" ] || { echo "no track matches '$1'"; exit 1; }
  for id in $ids; do
    db -x -v id="$id" <<'SQL'
SELECT id, title, artist, album, duration_seconds, acquisition_status, failure_reason,
       acquisition_provenance, audio_source_url, cardinality(rejected_source_keys) AS rejected_sources,
       added_at, acquisition_started_at, audio_ref IS NOT NULL AS has_audio
  FROM tracks WHERE id = :'id';
SQL
    if [ -n "$api" ]; then
      echo "-- log lines for $id in $api (this container's lifetime only)"
      docker logs "$api" 2>&1 | grep -F "$id" | cut -c1-400 | tail -60
    fi
  done
  ;;

client)
  since=${1:-24h}
  track=${2:-}
  db -v since="$since" -v track="$track" <<'SQL'
SELECT occurred_at::timestamp(0), event_type, payload->>'track_id' AS track_id,
       payload->>'action' AS action, payload->>'entry_point' AS entry_point,
       payload->>'outcome' AS outcome, payload->>'reason' AS reason,
       payload->>'status' AS status, payload->>'correlation_id' AS correlation_id,
       payload->>'from' AS from_status, payload->>'to' AS to_status,
       payload->>'source' AS source, left(payload->>'message', 150) AS message,
       left(payload->>'stack', 200) AS stack, payload->>'app_version' AS app_version
  FROM discovery_events
 WHERE event_type IN ('acquisition_ui', 'client_error')
   AND occurred_at > now() - (:'since')::interval
   AND (:'track' = '' OR payload->>'track_id' = :'track')
 ORDER BY occurred_at DESC LIMIT 200;
SQL
  ;;

probe)
  need_api
  [ $# -ge 1 ] || { echo "usage: probe <artist title>"; exit 3; }
  echo "== $api, yt-dlp $(docker exec "$api" yt-dlp --version)"
  in_api '
for engine in ytsearch5 scsearch5; do
  echo "== $engine: $1"
  yt --no-download --flat-playlist --print "%(url)s	%(title)s" -- "$engine:$1" 2>&1 |
    while IFS="	" read -r url title; do
      case $url in ERROR*) echo "  search failed: $url"; continue ;; esac
      got=$(yt --simulate --print "%(duration)ss %(uploader)s" -- "$url" 2>&1 | tail -1 | cut -c1-110)
      printf "  %-45.45s %s\n     -> %s\n" "$title" "$url" "$got"
    done
done' "$*"
  ;;

tools)
  need_api
  echo "== $api"
  docker exec "$api" sh -c 'for b in yt-dlp rip ffprobe ffmpeg; do printf "%-8s %s\n" "$b" "$($b --version 2>&1 | head -1 | cut -c1-60)"; done
    printf "%-8s %s\n" fpcalc "$(fpcalc -version 2>&1 | head -1)"
    echo "cookie file: ${YTDLP_COOKIE_FILE:-unset}  js runtime: ${YTDLP_JS_RUNTIME:-unset}  streamrip services: ${STREAMRIP_SERVICES:-none}"'
  # One long-lived public item per source; OK means the source can extract from this IP today.
  in_api '
status=0
for c in "youtube https://www.youtube.com/watch?v=jNQXAC9IVRw" "ytmusic https://music.youtube.com/watch?v=jNQXAC9IVRw" "soundcloud https://api.soundcloud.com/tracks/soundcloud%3Atracks%3A157015344"; do
  name=${c%% *}; url=${c#* }
  out=$(yt --simulate --print "%(duration)s" -- "$url" 2>&1 | tail -1)
  # 30s is the length of a SoundCloud Go+ preview: reachable, but not a usable source.
  case $out in
    30|30.0) echo "canary $name: PREVIEW ONLY (30s)" ;;
    [0-9]*) echo "canary $name: OK (${out}s)" ;;
    *) echo "canary $name: FAIL $(echo "$out" | cut -c1-140)"; status=1 ;;
  esac
done
exit $status'
  ;;

logs)
  need_api
  since=${1:-1h}; pattern=${2:-.}
  docker logs --since "$since" "$api" 2>&1 |
    grep -iE 'acqui|candidate|download|ytdlp|yt-dlp|streamrip|schedul|reject|verif|stale' |
    grep -v '"path":"/admin/acquisition"' |
    grep -E -- "$pattern" | cut -c1-400 | tail -200
  ;;

sql)
  [ $# -ge 1 ] || { echo "usage: sql <select>"; exit 3; }
  # One read statement only. A second statement could switch the read-only
  # guard off before writing, and a backslash is a psql meta-command (\! runs
  # a shell on the VM). A single statement inside a read-only transaction
  # cannot write: data-modifying CTEs and SELECT INTO are refused.
  q=$(printf '%s' "$*" | sed -E 's/[[:space:];]+$//')
  case $q in *\;*|*\\*) echo "acq-debug: sql takes one statement, no ';' or backslash"; exit 3 ;; esac
  printf '%s' "$q" | grep -qiE '^[[:space:]]*(select|with|explain|show|table|values)[[:space:](]' ||
    { echo "acq-debug: sql only runs SELECT / WITH / EXPLAIN / SHOW / TABLE / VALUES"; exit 3; }
  printf '%s;\n' "$q" | db
  ;;

*) echo "acq-debug: unknown command '$cmd' (try: help)"; exit 3 ;;
esac
REMOTE
