import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const script = join(dirname(fileURLToPath(import.meta.url)), '..', 'mock-ratchet.mjs');

const ONE_OWN_MOCK = "jest.mock('../store');\n";
const TWO_OWN_MOCKS = "jest.mock('../store');\njest.doMock(\n  '@shared/api',\n  () => ({}),\n);\n";

function fixture(t, files) {
  const root = mkdtempSync(join(tmpdir(), 'mock-ratchet-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  for (const [path, body] of Object.entries(files)) {
    mkdirSync(dirname(join(root, path)), { recursive: true });
    writeFileSync(join(root, path), body);
  }
  return root;
}

function ratchet(root, ...args) {
  const r = spawnSync('node', [script, ...args], {
    encoding: 'utf8',
    env: { ...process.env, MOCK_RATCHET_ROOT: root },
  });
  return { status: r.status, out: `${r.stdout}${r.stderr}` };
}

function baselineOf(root) {
  return readFileSync(join(root, 'mock-ratchet.baseline'), 'utf8');
}

test('--write records one line per own-module mock, path relative to the root', (t) => {
  const root = fixture(t, { 'src/a/__tests__/a.test.ts': TWO_OWN_MOCKS });

  assert.equal(ratchet(root, '--write').status, 0);

  assert.equal(
    baselineOf(root),
    'src/a/__tests__/a.test.ts ../store\nsrc/a/__tests__/a.test.ts @shared/api\n',
  );
});

test('a count equal to the baseline passes', (t) => {
  const root = fixture(t, {
    'src/a.test.ts': ONE_OWN_MOCK,
    'mock-ratchet.baseline': 'src/a.test.ts ../store\n',
  });

  assert.equal(ratchet(root).status, 0);
});

test('a count above the baseline fails and names the new mock', (t) => {
  const root = fixture(t, {
    'src/a.test.ts': TWO_OWN_MOCKS,
    'mock-ratchet.baseline': 'src/a.test.ts ../store\n',
  });

  const { status, out } = ratchet(root);

  assert.equal(status, 1);
  assert.match(out, /new violation: src\/a\.test\.ts @shared\/api/);
});

test('a lower count passes once its baseline line is removed with --write', (t) => {
  const root = fixture(t, {
    'src/a.test.ts': ONE_OWN_MOCK,
    'mock-ratchet.baseline': 'src/a.test.ts ../store\nsrc/a.test.ts @shared/api\n',
  });
  assert.equal(ratchet(root, '--write').status, 0);

  assert.equal(ratchet(root).status, 0);
  assert.equal(baselineOf(root), 'src/a.test.ts ../store\n');
});

test('a removed mock whose baseline line stays fails, so the count cannot creep back up', (t) => {
  const root = fixture(t, {
    'src/a.test.ts': ONE_OWN_MOCK,
    'mock-ratchet.baseline': 'src/a.test.ts ../store\nsrc/a.test.ts @shared/api\n',
  });

  const { status, out } = ratchet(root);

  assert.equal(status, 1);
  assert.match(out, /fixed: remove it from the baseline: src\/a\.test\.ts @shared\/api/);
});

test('third-party mocks do not count', (t) => {
  const root = fixture(t, {
    'src/a.test.ts': "jest.mock('expo-router');\njest.mock(\n  '@supabase/supabase-js',\n);\n",
    'jest/setup.js': "jest.doMock('react-native-safe-area-context', () => ({}));\n",
    'mock-ratchet.baseline': '',
  });

  assert.equal(ratchet(root).status, 0);
});

test('every alias counts, in src and jest alike', (t) => {
  const aliases = ['./x', '../x', '@shared/x', '@features/x', '@/x', '@testing/x'];
  const root = fixture(t, {
    'src/a.test.tsx': aliases.map((m) => `jest.mock('${m}');\n`).join(''),
    'jest/setup.js': "jest.mock('@testing/doubles');\n",
  });

  ratchet(root, '--write');

  const lines = baselineOf(root).trim().split('\n');
  assert.equal(lines.length, aliases.length + 1);
  assert.ok(lines.includes('jest/setup.js @testing/doubles'));
});
