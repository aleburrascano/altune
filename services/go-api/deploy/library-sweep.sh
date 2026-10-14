#!/usr/bin/env bash


set -euo pipefail

cd "$(dirname "$0")/.."
. deploy/prod_host.sh

STATE_DIR="${SWEEP_STATE_DIR:-$HOME/.local/state/altune-library-sweep}"
BIN_DIR="${SWEEP_BIN_DIR:-}"
VM_HOST="${SWEEP_VM_HOST:-ubuntu@altune.duckdns.org}"
VM_KEY="${SWEEP_VM_KEY:-$HOME/.ssh/altune-prod.key}"
VM_ENV_FILE="${SWEEP_VM_ENV_FILE:-~/altune/services/go-api/.env.production}"
VM_RETAG_PATH="${SWEEP_VM_RETAG_PATH:-/tmp/retag}"
MIN_INTERVAL_SECONDS="${SWEEP_MIN_INTERVAL_SECONDS:-3000}"
UUID_RE='^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'

DRY_RUN=""
BUILT_DIR=""
DSN=""
RUN_DIR=""
BIND_RUN="-"
RECONCILE_RUN="-"
STARTED_AT=""
SINCE=""

for arg in "$@"; do
    case "$arg" in
        --dry-run) DRY_RUN=1 ;;
        *) log "FAILED: unknown argument '$arg' (only --dry-run is accepted)"; exit 2 ;;
    esac
done

cleanup() {
    [ -z "$BUILT_DIR" ] || rm -rf -- "$BUILT_DIR"
}
trap cleanup EXIT

vm_name() {
    local host=${VM_HOST#*@}
    printf '%s' "${host%%.*}"
}

looks_like_prod_vm() {
    local full=${VM_HOST#*@} vm_ip local_ips
    [ "$(hostname)" = "$full" ] && return 0
    [ "$(hostname -s)" = "$(vm_name)" ] && return 0
    vm_ip=$(getent hosts "$full" | awk '{print $1; exit}' || true)
    local_ips=$(hostname -I | tr ' ' '\n')
    [ -n "$vm_ip" ] && grep -qxF "$vm_ip" <<<"$local_ips"
}

refuse_on_prod_vm() {
    if looks_like_prod_vm; then
        log "FAILED: do not run the library sweep from the prod VM, Deezer rate-blocks the prod IP and the app shares it; run it from the dev host"
        exit 1
    fi
}

ssh_opts() {
    printf '%s\n' -i "$VM_KEY" -o BatchMode=yes -o ConnectTimeout=15
}

vm_ssh() {
    local opts
    mapfile -t opts < <(ssh_opts)
    printf '%s\n' "$1" | ssh "${opts[@]}" "$VM_HOST" bash -s
}

vm_copy() {
    local opts
    mapfile -t opts < <(ssh_opts)
    scp -q "${opts[@]}" "$1" "$VM_HOST:$2"
}

fetch_into_dsn() {
    DSN=$(fetch_dsn)
}

fetch_dsn() {
    vm_ssh "grep -E '^DATABASE_URL=' $VM_ENV_FILE | head -1 | cut -d= -f2- | tr -d \"\\\"'\""
}

vm_goarch() {
    case "$(vm_ssh uname -m)" in
        aarch64|arm64) echo arm64 ;;
        x86_64|amd64) echo amd64 ;;
        *) log "FAILED: cannot tell the VM architecture"; return 1 ;;
    esac
}

act() {
    local description=$1
    shift
    if [ -n "$DRY_RUN" ]; then
        printf 'would: %s\n' "$description"
        return 0
    fi
    log "$description"
    "$@"
}

now_utc() {
    date -u +%Y-%m-%dT%H:%M:%SZ
}

last_success_file() {
    printf '%s/last-success' "$STATE_DIR"
}

read_since() {
    SINCE=""
    [ -f "$(last_success_file)" ] && SINCE=$(<"$(last_success_file)")
    return 0
}

seconds_since_last_success() {
    local last
    last=$(date -u -d "$SINCE" +%s)
    echo $(($(date -u +%s) - last))
}

swept_recently() {
    [ -n "$SINCE" ] && [ "$(seconds_since_last_success)" -lt "$MIN_INTERVAL_SECONDS" ]
}

record_success() {
    printf '%s\n' "$STARTED_AT" >"$(last_success_file).tmp"
    mv -- "$(last_success_file).tmp" "$(last_success_file)"
}

record_outcome() {
    printf '%s %s since=%s bind=%s reconcile=%s\n' "$STARTED_AT" "$1" "${SINCE:-never}" "$BIND_RUN" "$RECONCILE_RUN" >>"$STATE_DIR/sweeps.log"
}

build_tools() {
    BUILT_DIR=$(mktemp -d)
    BIN_DIR=$BUILT_DIR
    local tool goarch
    for tool in librarybind libraryderive libraryreconcile; do
        go build -o "$BIN_DIR/$tool" "./cmd/$tool"
    done
    goarch=$(vm_goarch)
    CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" go build -o "$BIN_DIR/retag" ./cmd/retag
}

prepare_tools() {
    if [ -n "$BIN_DIR" ]; then
        log "using the tools in $BIN_DIR"
    elif [ -n "$DRY_RUN" ]; then
        printf 'would: build librarybind, libraryderive, libraryreconcile and a static linux retag into a temp dir\n'
    else
        build_tools
    fi
}

run_bind() {
    LIBRARYBIND_DATABASE_URL="$DSN" "$BIN_DIR/librarybind" -apply -report "$RUN_DIR/relength-plan.json" | tee "$RUN_DIR/bind.out"
}

run_derive() {
    local since_args=()
    [ -z "$SINCE" ] || since_args=(-since "$SINCE")
    LIBRARYDERIVE_DATABASE_URL="$DSN" "$BIN_DIR/libraryderive" "${since_args[@]+"${since_args[@]}"}" -report "$RUN_DIR/derive-report.json"
}

run_reconcile() {
    RECONCILE_DATABASE_URL="$DSN" "$BIN_DIR/libraryreconcile" -report "$RUN_DIR/derive-report.json" -apply | tee "$RUN_DIR/reconcile.out"
}

run_id_in() {
    if [ -n "$DRY_RUN" ]; then
        printf '<%s run id>' "$3"
        return 0
    fi
    sed -n "s/$2/\\1/p" "$1" | head -1
}

retag_on_vm() {
    vm_ssh "cd ~/altune/services/go-api && img=\$(docker ps --format \"{{.Image}}\" | grep -E \"^altune-go-api:\" | head -1) && test -n \"\$img\" && docker run --rm --network host --env-file .env.production -e YTDLP_COOKIE_FILE= -e BEHAVIORAL_CORPUS_PATH= -v $VM_RETAG_PATH:/retag:ro --entrypoint /retag \$img -run $1 -apply"
}

log_unapplied_length_plan() {
    local plan="$RUN_DIR/relength-plan.json"
    [ -s "$plan" ] || return 0
    log "fix_isrc plan, logged and not applied (audio replacement is a manual step):"
    cat -- "$plan"
}

sweep() {
    log "sweep started $STARTED_AT, since ${SINCE:-the beginning}"
    prepare_tools
    act "fetch the prod database url from $VM_HOST" fetch_into_dsn
    if [ -z "$DRY_RUN" ] && [ -z "$DSN" ]; then
        log "FAILED: no DATABASE_URL in $VM_ENV_FILE on $VM_HOST"
        return 1
    fi
    act "bind: librarybind -apply -report $RUN_DIR/relength-plan.json" run_bind
    BIND_RUN=$(run_id_in "$RUN_DIR/bind.out" '^run \([0-9a-f-]\{36\}\) .*' bind)
    act "derive: libraryderive${SINCE:+ -since $SINCE} -report $RUN_DIR/derive-report.json" run_derive
    act "reconcile: libraryreconcile -report $RUN_DIR/derive-report.json -apply" run_reconcile
    RECONCILE_RUN=$(run_id_in "$RUN_DIR/reconcile.out" '^run_id \([0-9a-f-]\{36\}\)$' reconcile)
    if [ -z "$DRY_RUN" ] && ! [[ $RECONCILE_RUN =~ $UUID_RE ]]; then
        log "FAILED: libraryreconcile printed no run id, so there is nothing to retag"
        return 1
    fi
    act "copy the retag binary to $VM_HOST:$VM_RETAG_PATH" vm_copy "$BIN_DIR/retag" "$VM_RETAG_PATH"
    act "retag on the VM: retag -run $RECONCILE_RUN -apply" retag_on_vm "$RECONCILE_RUN"
    act "log the fix_isrc plan without applying it" log_unapplied_length_plan
    act "write $STARTED_AT to $(last_success_file)" record_success
    [ -n "$DRY_RUN" ] || log "sweep finished: bind=$BIND_RUN reconcile=$RECONCILE_RUN"
}

finish_sweep() {
    local rc=$?
    if [ "$rc" -eq 0 ]; then record_outcome ok; else record_outcome "failed($rc)"; fi
    cleanup
}

sweep_recorded() {
    trap finish_sweep EXIT
    sweep
}

sweep_dry_run() {
    STARTED_AT=$(now_utc)
    read_since
    RUN_DIR="$STATE_DIR/runs/$STARTED_AT"
    printf 'would: take the lock %s/lock and skip if another sweep holds it\n' "$STATE_DIR"
    sweep
}

sweep_for_real() {
    mkdir -p "$STATE_DIR"
    exec 9>"$STATE_DIR/lock"
    if ! flock -n 9; then
        log "SKIPPED: another sweep holds $STATE_DIR/lock"
        exit 0
    fi
    STARTED_AT=$(now_utc)
    read_since
    if swept_recently; then
        log "SKIPPED: the last sweep succeeded at $SINCE, less than ${MIN_INTERVAL_SECONDS}s ago"
        exit 0
    fi
    RUN_DIR="$STATE_DIR/runs/$STARTED_AT"
    mkdir -p "$RUN_DIR"
    sweep_recorded 2>&1 | tee -a "$RUN_DIR/sweep.log"
}

refuse_on_prod_vm
if [ -n "$DRY_RUN" ]; then
    sweep_dry_run
else
    sweep_for_real
fi
