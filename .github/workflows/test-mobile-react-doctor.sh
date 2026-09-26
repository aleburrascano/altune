#!/usr/bin/env bash

set -euo pipefail

npm run --silent doctor:ci > doctor-report.json || true
node -e "
  const max = Number(process.env.MAX_WARNINGS);
  const s = require('./doctor-report.json').summary;
  console.log('errors:', s.errorCount, '| warnings:', s.warningCount, '(ceiling', max + ')');
  if (s.errorCount > 0) { console.log('::error::react-doctor reported ' + s.errorCount + ' error(s)'); process.exit(1); }
  if (s.warningCount > max) { console.log('::error::react-doctor warnings rose to ' + s.warningCount + ', above the ' + max + ' ceiling'); process.exit(1); }
"
