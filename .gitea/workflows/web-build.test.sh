#!/usr/bin/env bash
cd "$(dirname "$0")" || exit 1
script=$PWD/web-build.sh
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin" "$work/wt/apps/mobile" "$work/wt/.github/workflows"

cat > "$work/bin/npx" <<'STUB'
#!/usr/bin/env bash
echo "$*" >> "$NPX_LOG"
STUB
chmod +x "$work/bin/npx"
for s in deploy-web-csp-check deploy-web-pack; do
  printf '#!/usr/bin/env bash\n' > "$work/wt/.github/workflows/$s.sh"
done

export NPX_LOG=$work/npx.log
PATH="$work/bin:$PATH" WT="$work/wt" bash "$script" || { echo "FAIL web-build exited non-zero"; exit 1; }
if grep -qE '^expo export -p web( .*)? --clear( |$)' "$NPX_LOG"; then
  echo "web-build tests ok"
else
  echo "FAIL expo export ran without --clear (shared Metro cache reuses another checkout's router root)"
  exit 1
fi
