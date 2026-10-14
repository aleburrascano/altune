import { test } from 'node:test';
import assert from 'node:assert/strict';

import { diffAgainstBaseline } from '../ratchet-baseline.mjs';

test('a repeated violation counts once per baseline line', () => {
  const diff = diffAgainstBaseline(['a', 'a', 'b'], ['a', 'c']);

  assert.deepEqual(diff, { added: ['a', 'b'], fixed: ['c'] });
});

test('a tree that matches its baseline has nothing added or fixed', () => {
  assert.deepEqual(diffAgainstBaseline(['a', 'b'], ['b', 'a']), { added: [], fixed: [] });
});
