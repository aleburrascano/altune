#!/usr/bin/env bash

set -euo pipefail

ceiling_file="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/client/react-doctor.ceiling"
MAX_WARNINGS=$(tr -d '[:space:]' <"$ceiling_file")
export MAX_WARNINGS
[[ "$MAX_WARNINGS" =~ ^[0-9]+$ ]] || { echo "::error::$ceiling_file must hold one whole number, got '$MAX_WARNINGS'"; exit 1; }

npm run --silent doctor:ci > doctor-report.json || true
node -e "
  const max = Number(process.env.MAX_WARNINGS);
  const s = require('./doctor-report.json').summary;
  console.log('errors:', s.errorCount, '| warnings:', s.warningCount, '(ceiling', max + ')');
  if (s.errorCount > 0) { console.log('::error::react-doctor reported ' + s.errorCount + ' error(s)'); process.exit(1); }
  if (s.warningCount > max) { console.log('::error::react-doctor warnings rose to ' + s.warningCount + ', above the ' + max + ' ceiling in client/react-doctor.ceiling'); process.exit(1); }
"
