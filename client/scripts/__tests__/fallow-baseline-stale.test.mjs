import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { staleBaselineEntries } from '../fallow-baseline-stale.mjs';

const scriptsDir = join(dirname(fileURLToPath(import.meta.url)), '..');
const script = join(scriptsDir, 'fallow-baseline-stale.mjs');
const committedBaseline = join(scriptsDir, '..', 'baselines', 'fallow-baseline.json');

const LIVE_REPORT = {
  unused_files: [{ path: 'src/a.ts' }],
  unused_exports: [{ path: 'src/b.ts', export_name: 'foo' }],
  unused_types: [{ path: 'src/c.ts', export_name: 'Bar' }],
  duplicate_exports: [
    { export_name: 'Dup', locations: [{ path: 'src/y.ts' }, { path: 'src/x.ts' }] },
  ],
};

function runCheck(baselineFile) {
  const r = spawnSync('node', [script], {
    encoding: 'utf8',
    env: { ...process.env, FALLOW_BASELINE_FILE: baselineFile },
  });
  return { status: r.status, out: `${r.stdout}${r.stderr}` };
}

test('a baseline holding only live findings has no stale entries', () => {
  const baseline = {
    unused_files: ['src/a.ts'],
    unused_exports: ['src/b.ts:foo'],
    unused_types: ['src/c.ts:Bar'],
    duplicate_exports: ['Dup|src/x.ts|src/y.ts'],
  };

  assert.deepEqual(staleBaselineEntries(baseline, LIVE_REPORT), []);
});

test('every baseline entry that is no longer a finding is listed by category', () => {
  const baseline = {
    unused_files: ['src/a.ts', 'src/gone.ts'],
    unused_exports: ['src/b.ts:foo', 'src/b.ts:renamed'],
    unused_types: ['src/c.ts:Gone'],
    duplicate_exports: ['Dup|src/x.ts|src/z.ts'],
    unused_dependencies: [],
  };

  assert.deepEqual(staleBaselineEntries(baseline, LIVE_REPORT), [
    'unused_files: src/gone.ts',
    'unused_exports: src/b.ts:renamed',
    'unused_types: src/c.ts:Gone',
    'duplicate_exports: Dup|src/x.ts|src/z.ts',
  ]);
});

test('the committed baseline has no stale entries against the live fallow report', () => {
  const result = runCheck(committedBaseline);

  assert.equal(result.status, 0, result.out);
});

test('the check fails and names the entry when the baseline lists a symbol that is no longer a finding', (t) => {
  const dir = mkdtempSync(join(tmpdir(), 'fallow-stale-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const baseline = JSON.parse(readFileSync(committedBaseline, 'utf8'));
  baseline.unused_exports.push('src/shared/gone.ts:vanishedExport');
  const staleBaseline = join(dir, 'fallow-baseline.json');
  writeFileSync(staleBaseline, JSON.stringify(baseline));

  const result = runCheck(staleBaseline);

  assert.equal(result.status, 1, result.out);
  assert.match(result.out, /unused_exports: src\/shared\/gone\.ts:vanishedExport/);
  assert.match(result.out, /save-baseline/);
});
