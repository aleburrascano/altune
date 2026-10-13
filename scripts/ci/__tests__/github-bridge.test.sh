#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
script=$PWD/github-bridge.sh
fail=0
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT

cat > "$work/curl" <<'STUB'
#!/usr/bin/env bash
url=${*: -1}
case $url in
  *"/pulls?"*"page=1") cat "$WORK/pulls.json" ;;
  *"/pulls?"*) echo '[]' ;;
  *"/statuses?"*) cat "$WORK/statuses.json" ;;
  *"/statuses/"*) printf '%s %s\n' "$url" "$*" >> "$WORK/posts.log" ;;
esac
STUB
cat > "$work/gh" <<'STUB'
#!/usr/bin/env bash
cat "$WORK/checks.json"
STUB
cat > "$work/git" <<'STUB'
#!/usr/bin/env bash
echo "$*" >> "$WORK/git.log"
case " $* " in
  *" ls-remote "*) cat "$WORK/remote.txt" ;;
  *" rev-parse "*) echo "$HEAD_SHA" ;;
esac
exit 0
STUB
chmod +x "$work/curl" "$work/gh" "$work/git"

run() {
  : > "$work/posts.log"; : > "$work/git.log"
  PATH="$work:$PATH" WORK="$work" HEAD_SHA=abc123 BRIDGE_GIT="$work/bridge.git" \
    GITEA_API=http://gitea/api/v1 GITEA_TOKEN=t REPO=o/r MIRROR_KEY=/k GITHUB_REMOTE=git@github.com:o/r.git \
    bash "$script" >"$work/out" 2>&1; got=$?
}

check() {
  local desc=$1 want=$2 have=$3
  [ "$want" = "$have" ] || { echo "FAIL $desc: want '$want' got '$have'"; fail=1; }
}

posted() { grep -c "\"state\":\"$1\"" "$work/posts.log"; }

echo '[{"number":7}]' > "$work/pulls.json"
echo '[]' > "$work/statuses.json"
: > "$work/remote.txt"
echo '{"check_runs":[]}' > "$work/checks.json"
run
check "new pr exits 0" "0" "$got"
grep -q 'push -q --force git@github.com:o/r.git abc123:refs/heads/pr/7' "$work/git.log" || { echo "FAIL new pr not pushed"; fail=1; }
check "new pr posts pending" "1" "$(posted pending)"
grep -q 'statuses/abc123' "$work/posts.log" || { echo "FAIL status not posted to sha"; fail=1; }
grep -qF 'github / pr-checks' "$work/posts.log" || { echo "FAIL context missing"; fail=1; }

printf 'abc123\trefs/heads/pr/7\n' > "$work/remote.txt"
echo '{"check_runs":[{"id":1,"name":"other","status":"completed","conclusion":"failure"},{"id":2,"name":"pr-checks","status":"completed","conclusion":"success","html_url":"http://gh/run/2"}]}' > "$work/checks.json"
run
check "up to date pr is not pushed" "0" "$(grep -c ' push ' "$work/git.log")"
check "success posts success" "1" "$(posted success)"
grep -qF '"target_url":"http://gh/run/2"' "$work/posts.log" || { echo "FAIL target_url missing"; fail=1; }

echo '{"check_runs":[{"id":2,"name":"pr-checks","status":"completed","conclusion":"cancelled"}]}' > "$work/checks.json"
run
check "failure posts failure" "1" "$(posted failure)"

echo '[{"id":9,"context":"github / pr-checks","status":"failure"},{"id":3,"context":"github / pr-checks","status":"pending"}]' > "$work/statuses.json"
run
check "unchanged state posts nothing" "0" "$(wc -l < "$work/posts.log")"

echo '[]' > "$work/pulls.json"
printf 'abc123\trefs/heads/pr/7\n' > "$work/remote.txt"
run
grep -q 'push -q git@github.com:o/r.git --delete refs/heads/pr/7' "$work/git.log" || { echo "FAIL closed pr branch not deleted"; fail=1; }

echo '[{"number":7}]' > "$work/pulls.json"
printf 'abc123\trefs/heads/pr/7\n' > "$work/remote.txt"
echo 'not json' > "$work/checks.json"
run
check "every pr failing exits 1" "1" "$got"

[ "$fail" = 0 ] && echo "github-bridge tests ok"
[ "$fail" = 0 ]
