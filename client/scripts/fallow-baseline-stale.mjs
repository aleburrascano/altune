import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const mobileRoot = path.join(path.dirname(fileURLToPath(import.meta.url)), '..');
const baselinePath =
  process.env.FALLOW_BASELINE_FILE ?? path.join(mobileRoot, 'baselines', 'fallow-baseline.json');
const MAX_FALLOW_OUTPUT_BYTES = 256 * 1024 * 1024;
const REGENERATE_HINT =
  'the finding is gone: run `cd client && npx fallow dead-code --save-baseline baselines/fallow-baseline.json` to drop it';

const entryKeyOf = {
  unused_files: (finding) => finding.path,
  unused_exports: (finding) => `${finding.path}:${finding.export_name}`,
  unused_types: (finding) => `${finding.path}:${finding.export_name}`,
  duplicate_exports: (finding) =>
    [finding.export_name, ...finding.locations.map((location) => location.path)].join('|'),
};

function normalizedKey(category, key) {
  if (category !== 'duplicate_exports') return key;
  const [exportName, ...paths] = key.split('|');
  return [exportName, ...paths.sort()].join('|');
}

export function liveEntryKeys(liveReport) {
  return Object.fromEntries(
    Object.entries(entryKeyOf).map(([category, keyOf]) => [
      category,
      new Set((liveReport[category] ?? []).map((finding) => normalizedKey(category, keyOf(finding)))),
    ]),
  );
}

export function staleBaselineEntries(baseline, liveReport) {
  const live = liveEntryKeys(liveReport);
  return Object.entries(baseline).flatMap(([category, entries]) => {
    if (!Array.isArray(entries) || entries.length === 0) return [];
    if (!(category in live)) return entries.map((entry) => `${category}: ${entry} (category cannot be checked)`);
    return entries
      .filter((entry) => !live[category].has(normalizedKey(category, entry)))
      .map((entry) => `${category}: ${entry}`);
  });
}

function runLiveReport() {
  const run = spawnSync('npx', ['--no-install', 'fallow', 'dead-code', '--format', 'json', '--quiet'], {
    cwd: mobileRoot,
    encoding: 'utf8',
    maxBuffer: MAX_FALLOW_OUTPUT_BYTES,
  });
  try {
    return JSON.parse(run.stdout);
  } catch {
    console.error(`fallow-baseline-stale: fallow printed no JSON report\n${run.stdout}${run.stderr}`);
    process.exit(2);
  }
}

function main() {
  const baseline = JSON.parse(fs.readFileSync(baselinePath, 'utf8'));
  const stale = staleBaselineEntries(baseline, runLiveReport());
  if (stale.length === 0) return;
  console.error(
    `fallow-baseline-stale: ${stale.length} baseline entries are no longer findings:\n${stale.map((entry) => `  ${entry}`).join('\n')}\n${REGENERATE_HINT}`,
  );
  process.exit(1);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) main();
