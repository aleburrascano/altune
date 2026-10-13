#!/usr/bin/env bash

set -euo pipefail

base=${1:-}
ceiling_file="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/client/baselines/react-doctor.ceiling"
MAX_WARNINGS=$(tr -d '[:space:]' <"$ceiling_file")
export MAX_WARNINGS
[[ "$MAX_WARNINGS" =~ ^[0-9]+$ ]] || { echo "::error::$ceiling_file must hold one whole number, got '$MAX_WARNINGS'"; exit 1; }

npm run --silent doctor:ci > doctor-report.json || true
node -e "
  const max = Number(process.env.MAX_WARNINGS);
  const s = require('./doctor-report.json').summary;
  console.log('errors:', s.errorCount, '| warnings:', s.warningCount, '(ceiling', max + ')');
  if (s.errorCount > 0) console.log('::error::react-doctor reported ' + s.errorCount + ' error(s)');
  if (s.warningCount > max) console.log('::error::react-doctor warnings rose to ' + s.warningCount + ', above the ' + max + ' ceiling in client/baselines/react-doctor.ceiling');
  process.exit(s.errorCount > 0 || s.warningCount > max ? 1 : 0);
" && exit 0

if [ -n "$base" ]; then
  node scripts/react-doctor-changed-lines.mjs --list-new "$base" doctor-report.json || true
else
  node -e "
    for (const d of require('./doctor-report.json').diagnostics.filter((d) => d.severity === 'error'))
      console.log('  ' + d.filePath + ':' + d.line + ' ' + d.plugin + '/' + d.rule + ' ' + d.message);
  " || true
  echo "pass a base ref to list the warnings new versus it: bash scripts/ci/test-mobile-react-doctor.sh <base>; full report: client/doctor-report.json"
fi
exit 1
