#!/usr/bin/env bash
usage() {
  cat <<'USAGE'
Debug audio acquisition against the live backend from a terminal, without the
app. Everything runs on the OCI VM over SSH and is read-only: the database
session is forced read-only, yt-dlp runs with --simulate against a throwaway
copy of the cookie file, and no secret is ever printed.

Usage: bash scripts/acq-debug.sh [--staging] <command> [args]
  summary [days]        status mix, failure codes, which sources delivered, stuck pending (default 14 days)
  track <text|uuid>     a track's acquisition row(s), then its log lines from the running go-api
  capture <title|uuid>  replay a track's rejection log through cmd/acquisitioneval's evaluator
  client [since] [track]  acquisition_ui/client_error events from discovery_events (default
                        since 24 hours ago; track filters on the event's track_id)
  probe <query>         run the app's own yt-dlp search for <query>, then try extracting each
                        candidate the way the download step would — shows bot-checks, previews, dead ids
  tools                 binary versions and a live canary per source (is YouTube / SoundCloud reachable?)
  logs [since] [regex]  acquisition log lines from the running go-api (default since 1h)
  sql <select>          an ad-hoc read-only query against the tier's database

Env: ALTUNE_HOST (required: user@host or an ~/.ssh/config alias for the VM),
     ALTUNE_SSH_KEY (optional identity file; else ssh's config and agent decide),
     ALTUNE_REMOTE_DIR (checkout on the VM, relative to the remote home; default altune).
Exit: 0 ok, 1 a check failed, 3 could not run (no SSH, no container, bad usage).
USAGE
}
set -uo pipefail

tier=prod
if [ "${1:-}" = --staging ]; then tier=staging; shift; fi
if [ $# -eq 0 ] || [ "$1" = help ] || [ "$1" = -h ]; then
  usage
  exit 0
fi

host=${ALTUNE_HOST:-}
[ -n "$host" ] || { echo "acq-debug: set ALTUNE_HOST to the VM's ssh target (user@host or an ssh config alias)" >&2; exit 3; }
ssh_opts=(-o BatchMode=yes -o ConnectTimeout=10)
[ -n "${ALTUNE_SSH_KEY:-}" ] && ssh_opts=(-i "$ALTUNE_SSH_KEY" "${ssh_opts[@]}")
remote_dir=${ALTUNE_REMOTE_DIR:-altune}

run_remote() {
  ssh "${ssh_opts[@]}" "$host" \
    "bash -s -- $(printf '%q ' "$remote_dir" "$tier" "$@")" <<'REMOTE'
set -uo pipefail
dir=$1; tier=$2; cmd=$3; shift 3
cd "$dir/services/go-api" || { echo "acq-debug: no checkout at $dir on the VM"; exit 3; }

case $tier in
  prod)    prefix=altune-go-api;         envfile=.env.production ;;
  staging) prefix=altune-staging-go-api; envfile=.env.staging ;;
esac
api=$(docker ps --filter "name=^${prefix}-" --format '{{.Names}}' | head -1)

need_api() { [ -n "$api" ] || { echo "acq-debug: no running ${prefix}-* container" >&2; exit 3; }; }

db() {
  local url
  url=$(grep -E '^DATABASE_URL=' "$envfile" | head -1 | cut -d= -f2- | tr -d "\"'")
  [ -n "$url" ] || { echo "acq-debug: no DATABASE_URL in $envfile"; exit 3; }
  { echo "SET default_transaction_read_only = on; SET statement_timeout = '30s';"; cat; } |
    psql "$url" -X -q -v ON_ERROR_STOP=1 "$@"
}

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

capture)
  need_api
  [ $# -ge 1 ] || { echo "usage: capture <title|uuid>" >&2; exit 3; }
  id=$(db -A -t -v q="$1" <<'SQL' | grep -E '^[0-9a-f-]{36}$' | head -1
SELECT id FROM tracks WHERE id::text = :'q' OR title ILIKE '%' || :'q' || '%' OR artist ILIKE '%' || :'q' || '%'
 ORDER BY added_at DESC LIMIT 5;
SQL
)
  [ -n "$id" ] || { echo "no track matches '$1'" >&2; exit 1; }
  track_json=$(db -A -t -v id="$id" <<'SQL'
SELECT json_build_object(
         'id', id, 'title', title, 'artist', artist, 'album', album,
         'duration_seconds', duration_seconds, 'isrc', isrc,
         'acquisition_status', acquisition_status, 'failure_reason', failure_reason,
         'audio_source_url', audio_source_url)
  FROM tracks WHERE id = :'id';
SQL
)
  [ -n "$track_json" ] || { echo "acq-debug: no track row for $id" >&2; exit 1; }
  lines=$(docker logs "$api" 2>&1 | grep -F "$id" | grep -E '"msg":"(candidate_evaluated|acquisition\.rejection_summary)"')
  logs_json=""
  [ -n "$lines" ] && logs_json=$(printf '%s\n' "$lines" | paste -sd, -)
  printf '{"track": %s, "logs": [%s]}\n' "$track_json" "$logs_json"
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
  in_api '
status=0
for c in "youtube https://www.youtube.com/watch?v=jNQXAC9IVRw" "ytmusic https://music.youtube.com/watch?v=jNQXAC9IVRw" "soundcloud https://api.soundcloud.com/tracks/soundcloud%3Atracks%3A157015344"; do
  name=${c%% *}; url=${c#* }
  out=$(yt --simulate --print "%(duration)s" -- "$url" 2>&1 | tail -1)
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
    grep -v '"path":"/observe/acquisition"' |
    grep -E -- "$pattern" | cut -c1-400 | tail -200
  ;;

sql)
  [ $# -ge 1 ] || { echo "usage: sql <select>"; exit 3; }
  q=$(printf '%s' "$*" | sed -E 's/[[:space:];]+$//')
  case $q in *\;*|*\\*) echo "acq-debug: sql takes one statement, no ';' or backslash"; exit 3 ;; esac
  printf '%s' "$q" | grep -qiE '^[[:space:]]*(select|with|explain|show|table|values)[[:space:](]' ||
    { echo "acq-debug: sql only runs SELECT / WITH / EXPLAIN / SHOW / TABLE / VALUES"; exit 3; }
  printf '%s;\n' "$q" | db
  ;;

*) echo "acq-debug: unknown command '$cmd' (try: help)"; exit 3 ;;
esac
REMOTE
}

if [ "${1:-}" = capture ]; then
  root=$(cd "$(dirname "$0")/.." && pwd)
  dump=$(run_remote "$@")
  status=$?
  [ $status -eq 0 ] || exit $status
  bin=$(mktemp)
  trap 'rm -f "$bin"' EXIT
  (cd "$root/services/go-api" && go build -o "$bin" ./cmd/acquisitioneval) || exit 1
  printf '%s' "$dump" | "$bin" capture -tier "$tier"
  exit $?
fi

run_remote "$@"
exit $?
