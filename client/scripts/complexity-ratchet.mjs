import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const mobileRoot = path.join(path.dirname(fileURLToPath(import.meta.url)), '..');
const baselinePath =
  process.env.COMPLEXITY_BASELINE_FILE ?? path.join(mobileRoot, 'baselines', 'complexity.baseline');

export function findingKey(finding) {
  return `${finding.path} ${finding.name}`;
}

export function sortedKeys(findings) {
  const input = findings.map((finding) => `${findingKey(finding)}\n`).join('');
  const result = spawnSync('sort', { input, encoding: 'utf8', maxBuffer: 256 * 1024 * 1024 });
  return result.stdout.split('\n').filter((line) => line !== '');
}

export function readBaseline(file = baselinePath) {
  return fs
    .readFileSync(file, 'utf8')
    .split('\n')
    .filter((line) => line !== '');
}

export function compareToBaseline(findings, baseline) {
  const remaining = new Map();
  for (const key of baseline) remaining.set(key, (remaining.get(key) ?? 0) + 1);
  const added = [];
  for (const finding of findings) {
    const key = findingKey(finding);
    const left = remaining.get(key) ?? 0;
    if (left === 0) added.push(finding);
    else remaining.set(key, left - 1);
  }
  const fixed = [...remaining].flatMap(([key, count]) => Array(count).fill(key)).sort();
  return { added, fixed };
}

function runFallow() {
  const result = spawnSync('npx', ['fallow', 'health', '--format', 'json'], {
    cwd: mobileRoot,
    encoding: 'utf8',
    maxBuffer: 256 * 1024 * 1024,
  });
  let report;
  try {
    report = JSON.parse(result.stdout);
  } catch {
    report = null;
  }
  if (!report || !Array.isArray(report.findings)) {
    console.error(
      '::error::could not parse fallow health output; the complexity ratchet verified nothing',
    );
    process.exit(1);
  }
  return report.findings;
}

function main() {
  const findings = runFallow();
  if (process.argv.includes('--write')) {
    const keys = sortedKeys(findings);
    fs.writeFileSync(baselinePath, keys.map((key) => `${key}\n`).join(''));
    console.log(`complexity-ratchet: wrote ${keys.length} findings to the baseline`);
    return;
  }
  const { added, fixed } = compareToBaseline(findings, readBaseline());
  for (const finding of added) {
    console.log(`::error::new complexity finding: ${findingKey(finding)} (line ${finding.line})`);
  }
  for (const key of fixed) console.log(`fixed: remove it from the baseline: ${key}`);
  console.log(
    `complexity findings: ${findings.length}, new: ${added.length}, fixed: ${fixed.length}`,
  );
  process.exit(added.length > 0 ? 1 : 0);
}

if (import.meta.url === `file://${process.argv[1]}`) {
  main();
}
