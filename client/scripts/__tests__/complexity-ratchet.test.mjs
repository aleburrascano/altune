import { test } from 'node:test';
import assert from 'node:assert/strict';
import { compareToBaseline, sortedKeys } from '../complexity-ratchet.mjs';

const finding = (path, name, line) => ({ path, name, line });

test('a finding missing from the baseline fails and is reported with its line', () => {
  const { added, fixed } = compareToBaseline(
    [finding('src/a.ts', 'foo', 10), finding('src/b.ts', 'bar', 42)],
    ['src/a.ts foo'],
  );
  assert.deepEqual(added, [finding('src/b.ts', 'bar', 42)]);
  assert.deepEqual(fixed, []);
});

test('a baseline key no longer found is reported as fixed and does not add', () => {
  const { added, fixed } = compareToBaseline(
    [finding('src/a.ts', 'foo', 10)],
    ['src/a.ts foo', 'src/b.ts bar'],
  );
  assert.deepEqual(added, []);
  assert.deepEqual(fixed, ['src/b.ts bar']);
});

test('the same function on another line is neither new nor fixed', () => {
  const { added, fixed } = compareToBaseline([finding('src/a.ts', 'foo', 99)], ['src/a.ts foo']);
  assert.deepEqual(added, []);
  assert.deepEqual(fixed, []);
});

test('a second finding with an already baselined key is new', () => {
  const { added } = compareToBaseline(
    [finding('src/a.ts', 'foo', 1), finding('src/a.ts', 'foo', 2)],
    ['src/a.ts foo'],
  );
  assert.deepEqual(added, [finding('src/a.ts', 'foo', 2)]);
});

test('written keys are sorted', () => {
  const keys = sortedKeys([
    finding('src/z.ts', 'a', 1),
    finding('src/a.ts', 'b', 2),
    finding('src/a.ts', 'a', 3),
  ]);
  assert.deepEqual(keys, ['src/a.ts a', 'src/a.ts b', 'src/z.ts a']);
});
