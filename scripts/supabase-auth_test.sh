#!/usr/bin/env bash
set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
AUTH_DIR="$HERE/../supabase/auth"
DUMMY_TOKEN=sbp_dummy_test_token_0000
FAILURES=0
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

mkdir -p "$WORK/bin" "$WORK/home"
cat >"$WORK/bin/curl" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$STUB_DIR/argv.log"
cat >"$STUB_DIR/config.log"
method=GET out=/dev/null body=
url=${*: -1}
while [ $# -gt 0 ]; do
    case "$1" in
        -X) method=$2; shift ;;
        -o) out=$2; shift ;;
        --data-binary) body=${2#@}; shift ;;
    esac
    shift
done
case "$method $url" in
    "GET https://api.supabase.com/v1/projects/ijyjoyxhwmbmriwzazbx/config/auth" | \
        "GET https://api.supabase.com/v1/projects/ellvexundmgvbbfqbzau/config/auth")
        cp "$STUB_DIR/live.json" "$out"
        printf '%s' "${STUB_GET_CODE:-200}" ;;
    "PATCH https://api.supabase.com/v1/projects/ijyjoyxhwmbmriwzazbx/config/auth")
        cp "$body" "$STUB_DIR/patch.json"
        if [ "${STUB_PATCH_CODE:-200}" = 200 ] && [ -z "${STUB_PATCH_DROPPED:-}" ]; then
            jq -s '.[0] + .[1]' "$STUB_DIR/live.json" "$body" >"$STUB_DIR/next.json"
            mv "$STUB_DIR/next.json" "$STUB_DIR/live.json"
        fi
        printf '{"message":"stub"}' >"$out"
        printf '%s' "${STUB_PATCH_CODE:-200}" ;;
    *) printf '%s' 404 ;;
esac
STUB
chmod +x "$WORK/bin/curl"

fail() {
    printf 'FAIL: %s\n      %s\n' "$CASE" "$1"
    FAILURES=$((FAILURES + 1))
}

hash_of() {
    printf '%s' "$1" | sha256sum | cut -d' ' -f1
}

tier_in_sync() {
    jq -s --arg google_hash "$(hash_of google)" '
        (.[0] + .[1]) as $merged
        | ($merged["$secrets"] | map({(.): null}) | add) as $no_secrets
        | ($merged["$ignore"] | map({(.): "platform"}) | add) as $platform
        | $merged | del(.["$secrets"], .["$ignore"])
        | . + $no_secrets + $platform + {external_google_secret: $google_hash}
    ' "$AUTH_DIR/base.json" "$AUTH_DIR/$1.json"
}

tier_live_with() {
    rm -f "$WORK/patch.json" "$WORK/argv.log"
    tier_in_sync "$1" | jq "$2" >"$WORK/live.json" || fail "could not build the stub's live config"
}

live_with() {
    tier_live_with staging "$1"
}

run_tool() {
    env PATH="$WORK/bin:$PATH" STUB_DIR="$WORK" HOME="$WORK/home" SUPABASE_ACCESS_TOKEN="$DUMMY_TOKEN" \
        bash "$HERE/supabase-auth.sh" "$@" <"$WORK/answer" >"$WORK/out.log" 2>&1
    RC=$?
}

answer() {
    printf '%s\n' "$1" >"$WORK/answer"
}

expect_rc() {
    [ "$RC" = "$1" ] || fail "expected exit $1, got $RC ($(cat "$WORK/out.log"))"
}

expect_line() {
    grep -qxF -- "$1" "$WORK/out.log" || fail "no line '$1' in: $(cat "$WORK/out.log")"
}

expect_no_patch() {
    [ ! -e "$WORK/patch.json" ] || fail "sent a PATCH: $(cat "$WORK/patch.json")"
}

answer ""

CASE="diff staging against a live config matching the repo exits 0 with no drift"
live_with .
run_tool diff staging
expect_rc 0
expect_line "no drift"

CASE="diff staging reports disable_signup drift"
live_with '.disable_signup = false'
run_tool diff staging
expect_rc 1
expect_line "disable_signup: false → true"

CASE="diff staging reports an unlisted live key as NEW KEY"
live_with '. + {brand_new_key: "x"}'
run_tool diff staging
expect_rc 1
expect_line "NEW KEY brand_new_key"

CASE="apply staging sends only the differing managed key, never a secret"
live_with '.disable_signup = false | .external_google_secret = "'"$(hash_of other)"'"'
answer y
run_tool apply staging
expect_rc 0
[ "$(jq -c . "$WORK/patch.json" 2>/dev/null)" = '{"disable_signup":true}' ] ||
    fail "PATCH body was not exactly {\"disable_signup\":true}: $(cat "$WORK/patch.json" 2>/dev/null)"
expect_line "applied 1 change(s) to staging"
answer ""

CASE="diff staging reports an enabled provider whose secret is null as unset"
live_with '.external_google_enabled = true | .external_google_secret = null'
run_tool diff staging
expect_rc 1
expect_line "external_google_secret: unset"

CASE="no secret key holds a value in supabase/auth/*.json"
secret_names=$(jq -c '.["$secrets"]' "$AUTH_DIR/base.json" 2>/dev/null)
if [ "$(jq 'length' <<<"${secret_names:-[]}")" != 36 ]; then
    fail "base.json does not list the 36 secret names"
fi
for file in "$AUTH_DIR"/*.json; do
    leaked=$(jq -r --argjson secrets "${secret_names:-[]}" 'keys[] | select(IN($secrets[]))' "$file" 2>&1)
    [ -z "$leaked" ] || fail "$(basename "$file") holds secret key(s): $leaked"
    ! grep -qE '[0-9a-f]{64}' "$file" || fail "$(basename "$file") holds a 64-hex value"
done

CASE="diff prod against prod's live snapshot exits 0 with no drift"
tier_live_with prod .
run_tool diff prod
expect_rc 0
expect_line "no drift"
grep -qF '/projects/ellvexundmgvbbfqbzau/config/auth' "$WORK/argv.log" || fail "diff prod did not GET the prod project"

CASE="diff prod reports staging's live config as drift"
tier_live_with staging .
run_tool diff prod
expect_rc 1
expect_line 'disable_signup: true → false'

CASE="CI runs this suite on changes to the script or the auth config"
ci_dir="$HERE/../.gitea/workflows"
grep -qE '^ +run: .*bash scripts/supabase-auth_test\.sh$' "$ci_dir/ci.yml" ||
    fail "ci.yml has no step running scripts/supabase-auth_test.sh"
for path in scripts/supabase-auth.sh supabase/auth/base.json; do
    flags=$(cd "$WORK" && git init -q ci && cd ci && git -c user.email=t@t -c user.name=t commit -q --allow-empty -m base &&
        mkdir -p "$(dirname "$path")" && : >"$path" && git add . &&
        git -c user.email=t@t -c user.name=t commit -q -m change &&
        bash "$ci_dir/ci-changes.sh" HEAD~1)
    grep -qx 'backend=true' <<<"$flags" || fail "a change to $path does not flag backend: $flags"
    rm -rf "$WORK/ci"
done

CASE="a difference on a platform-managed key is not drift"
live_with '.db_max_pool_size = 999 | .nimbus_oauth_client_secret = "'"$(hash_of nimbus)"'"'
run_tool diff staging
expect_rc 0

CASE="a disabled provider with a null secret is not drift"
live_with '.external_github_enabled = false | .external_github_secret = null'
run_tool diff staging
expect_rc 0

CASE="null is a value: a managed key that is null live but set in the repo is drift"
live_with '.password_min_length = null'
run_tool diff staging
expect_rc 1
expect_line "password_min_length: null → 8"

CASE="a managed key missing from the live config is drift"
live_with 'del(.password_min_length)'
run_tool diff staging
expect_rc 1
expect_line "password_min_length: absent → 8"

CASE="apply staging declined sends nothing and exits 1"
live_with '.disable_signup = false'
answer n
run_tool apply staging
expect_rc 1
grep -qF 'Apply 1 change(s) to staging? [y/N]' "$WORK/out.log" || fail "no prompt: $(cat "$WORK/out.log")"
expect_no_patch
answer ""

CASE="apply staging with no answer on stdin sends nothing"
live_with '.disable_signup = false'
: >"$WORK/answer"
run_tool apply staging
expect_rc 1
expect_no_patch
answer ""

CASE="apply staging with only secret drift sends nothing and exits 0"
live_with '.external_google_secret = null'
answer y
run_tool apply staging
expect_rc 0
expect_line "external_google_secret: unset"
expect_no_patch
answer ""

CASE="apply staging with nothing to change exits 0 without a PATCH"
live_with .
run_tool apply staging
expect_rc 0
expect_line "no drift"
expect_no_patch

CASE="apply staging run twice changes nothing the second time"
live_with '.disable_signup = false'
answer y
run_tool apply staging
expect_rc 0
rm -f "$WORK/patch.json"
run_tool apply staging
expect_rc 0
expect_line "no drift"
expect_no_patch
answer ""

CASE="apply staging exits 1 when the PATCH is rejected"
live_with '.disable_signup = false'
answer y
STUB_PATCH_CODE=500 run_tool apply staging
expect_rc 1
grep -qF 'PATCH staging auth config failed: HTTP 500' "$WORK/out.log" || fail "no PATCH failure line: $(cat "$WORK/out.log")"
answer ""

CASE="apply staging exits 1 when an accepted PATCH did not change the live config"
live_with '.disable_signup = false'
answer y
STUB_PATCH_DROPPED=1 run_tool apply staging
expect_rc 1
grep -qF '1 of 1 change(s) still differ on staging after the PATCH' "$WORK/out.log" ||
    fail "no unapplied-change line: $(cat "$WORK/out.log")"
answer ""

CASE="diff staging exits 2 and names the status when the GET fails"
live_with .
STUB_GET_CODE=401 run_tool diff staging
expect_rc 2
grep -qF 'GET staging auth config failed: HTTP 401' "$WORK/out.log" || fail "no GET failure line: $(cat "$WORK/out.log")"

CASE="the token never reaches curl's argv or the output"
live_with '.disable_signup = false'
answer y
run_tool apply staging
! grep -qF "$DUMMY_TOKEN" "$WORK/argv.log" || fail "token on curl's command line"
! grep -qF "$DUMMY_TOKEN" "$WORK/out.log" || fail "token in the output"
! grep -qF "$(hash_of google)" "$WORK/out.log" || fail "a secret hash in the output"
answer ""

CASE="without a token the tool exits 2 and names where the token goes"
live_with .
env PATH="$WORK/bin:$PATH" STUB_DIR="$WORK" HOME="$WORK/home" SUPABASE_ACCESS_TOKEN= \
    bash "$HERE/supabase-auth.sh" diff staging </dev/null >"$WORK/out.log" 2>&1
RC=$?
expect_rc 2
grep -qF 'SUPABASE_ACCESS_TOKEN' "$WORK/out.log" || fail "error does not name the token source: $(cat "$WORK/out.log")"

CASE="the token file is read when SUPABASE_ACCESS_TOKEN is unset"
mkdir -p "$WORK/home/.supabase"
printf '%s\n' "$DUMMY_TOKEN" >"$WORK/home/.supabase/access-token"
env PATH="$WORK/bin:$PATH" STUB_DIR="$WORK" HOME="$WORK/home" SUPABASE_ACCESS_TOKEN= \
    bash "$HERE/supabase-auth.sh" diff staging </dev/null >"$WORK/out.log" 2>&1
RC=$?
expect_rc 0
grep -qxF "header = \"Authorization: Bearer $DUMMY_TOKEN\"" "$WORK/config.log" ||
    fail "the token file's token did not reach curl"
rm -rf "$WORK/home/.supabase"

CASE="an unknown tier is a usage error"
run_tool diff dev
expect_rc 2

CASE="diff staging never sends a PATCH, even with drift"
live_with '.disable_signup = false | .password_min_length = 6'
answer y
run_tool diff staging
expect_rc 1
expect_no_patch
! grep -qF 'PATCH' "$WORK/argv.log" || fail "diff called curl with PATCH: $(cat "$WORK/argv.log")"
answer ""

CASE="apply staging never sends a platform-managed key"
live_with '.disable_signup = false | .db_max_pool_size = 999 | .api_max_request_duration = 1'
answer y
run_tool apply staging
[ "$(jq -c . "$WORK/patch.json" 2>/dev/null)" = '{"disable_signup":true}' ] ||
    fail "PATCH body was not exactly {\"disable_signup\":true}: $(cat "$WORK/patch.json" 2>/dev/null)"
answer ""

CASE="apply staging never sends an unlisted live key"
live_with '.disable_signup = false | . + {brand_new_key: "x"}'
answer y
run_tool apply staging
[ "$(jq -c . "$WORK/patch.json" 2>/dev/null)" = '{"disable_signup":true}' ] ||
    fail "PATCH body was not exactly {\"disable_signup\":true}: $(cat "$WORK/patch.json" 2>/dev/null)"
answer ""

CASE="apply staging counts and sends every differing managed key"
live_with '.disable_signup = false | .password_min_length = 6'
answer y
run_tool apply staging
expect_rc 0
grep -qF 'Apply 2 change(s) to staging? [y/N]' "$WORK/out.log" || fail "no 2-change prompt: $(cat "$WORK/out.log")"
[ "$(jq -cS . "$WORK/patch.json" 2>/dev/null)" = '{"disable_signup":true,"password_min_length":8}' ] ||
    fail "PATCH body was not both keys: $(cat "$WORK/patch.json" 2>/dev/null)"
answer ""

CASE="apply staging sends nothing when the GET fails"
live_with '.disable_signup = false'
answer y
STUB_GET_CODE=500 run_tool apply staging
[ "$RC" != 0 ] || fail "apply exited 0 after a failed GET"
expect_no_patch
! grep -qF 'PATCH' "$WORK/argv.log" || fail "apply sent a PATCH after a failed GET"
answer ""

CASE="apply prod targets the prod project, never staging"
tier_live_with prod '.disable_signup = true'
answer y
run_tool apply prod
grep -F 'PATCH' "$WORK/argv.log" | grep -qF '/projects/ellvexundmgvbbfqbzau/config/auth' ||
    fail "apply prod did not PATCH the prod project: $(cat "$WORK/argv.log")"
! grep -qF 'ijyjoyxhwmbmriwzazbx' "$WORK/argv.log" || fail "apply prod touched the staging project: $(cat "$WORK/argv.log")"
answer ""

CASE="apply never runs supabase config push"
cat >"$WORK/bin/supabase" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$STUB_DIR/supabase.log"
STUB
chmod +x "$WORK/bin/supabase"
live_with '.disable_signup = false'
answer y
run_tool apply staging
[ ! -e "$WORK/supabase.log" ] || fail "apply ran the supabase CLI: $(cat "$WORK/supabase.log")"
rm -f "$WORK/bin/supabase" "$WORK/supabase.log"
answer ""

CASE="a managed key whose live type differs from the repo is drift"
live_with '.password_min_length = "8"'
run_tool diff staging
expect_rc 1
expect_line 'password_min_length: "8" → 8'

CASE="a hostile live key name is printed literally, never executed"
live_with '. + {"$(touch pwned)": 1}'
RC=$(cd "$WORK" && env PATH="$WORK/bin:$PATH" STUB_DIR="$WORK" HOME="$WORK/home" SUPABASE_ACCESS_TOKEN="$DUMMY_TOKEN" \
    bash "$HERE/supabase-auth.sh" diff staging <"$WORK/answer" >"$WORK/out.log" 2>&1; echo $?)
expect_rc 1
expect_line 'NEW KEY $(touch pwned)'
[ ! -e "$WORK/pwned" ] || fail "a live key name was executed"

if [ "$FAILURES" -gt 0 ]; then
    printf '\n%d supabase-auth check(s) failed\n' "$FAILURES"
    exit 1
fi
printf 'supabase-auth: all checks passed\n'
