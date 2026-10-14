import fs from 'node:fs';

export function readBaselineLines(file) {
  return fs
    .readFileSync(file, 'utf8')
    .split('\n')
    .filter((line) => line !== '');
}

export function diffAgainstBaseline(found, baseline) {
  const remaining = new Map();
  for (const line of baseline) remaining.set(line, (remaining.get(line) ?? 0) + 1);
  const added = [];
  for (const line of found) {
    const left = remaining.get(line) ?? 0;
    if (left === 0) added.push(line);
    else remaining.set(line, left - 1);
  }
  const fixed = [...remaining].flatMap(([line, count]) => Array(count).fill(line));
  return { added, fixed };
}

function printBaselineReport({ added, fixed }, updateHint) {
  added.forEach((line) => console.log(`new violation: ${line}`));
  fixed.forEach((line) => console.log(`fixed: remove it from the baseline: ${line}`));
  if (fixed.length > 0 && added.length === 0) console.log(updateHint);
}

export function exitWithBaselineReport(diff, updateHint) {
  printBaselineReport(diff, updateHint);
  const failed = diff.added.length > 0 || diff.fixed.length > 0;
  process.exit(failed ? 1 : 0);
}
