#!/usr/bin/env bash
set -euo pipefail
case " ${MODES:-all} " in
  *" all "* | *" $MODE "*) ;;
  *) echo "$MODE not selected ($MODES)"; exit 0 ;;
esac
cd "$WT/services/go-api"
. "$WT/.gitea/workflows/eval-env.sh" production || exit 0
export CORPUS=cmd/discoveryeval/corpus-library.json
if [ "$MODE" != detail ] && [ ! -f "$CORPUS" ]; then
  echo "::notice::$MODE skipped: $CORPUS is not committed yet; dispatch discovery-eval-corpus-refresh with open-pr true and merge its PR first"
  exit 0
fi
mkdir -p tmp
go build -o ./tmp/discoveryeval ./cmd/discoveryeval
if [ "$MODE" = correction ]; then
  cid=$(docker run -d --rm -p 127.0.0.1::6379 redis:7)
  trap 'docker rm -f "$cid" >/dev/null 2>&1' EXIT
  port=$(docker port "$cid" 6379/tcp | head -1 | awk -F: '{print $NF}')
  export REDIS_URL="redis://127.0.0.1:$port"
  for _ in $(seq 30); do docker exec "$cid" redis-cli ping >/dev/null 2>&1 && break; sleep 1; done
  ./tmp/discoveryeval -mode correction-seed -corpus-file "$CORPUS"
else
  export REDIS_URL=
fi
rc=0
if [ -n "${REBASELINE:-}" ]; then
  ./tmp/discoveryeval -mode "$MODE" -corpus-file "$CORPUS" -update-baselines -noise-runs "${NOISE_RUNS:-3}" || rc=$?
else
  bash "$WT/.github/workflows/discovery-eval-mode.sh" || rc=$?
  mkdir -p "$RUN_DIR"
  cp ./tmp/*-"$MODE".json "$RUN_DIR"/ 2>/dev/null || true
fi
exit "$rc"
