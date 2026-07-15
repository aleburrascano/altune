import { reconstructPlayOrder, resolveResumeStartIndex } from '../resumeQueue';

describe('resolveResumeStartIndex', () => {
  it('returns the saved index when nothing was filtered out', () => {
    const saved = ['a', 'b', 'c'];
    expect(resolveResumeStartIndex(saved, 2, ['a', 'b', 'c'])).toBe(2);
  });

  it('relocates the current track when an earlier track was dropped (filter shift)', () => {
    // 'a' was not ready and got filtered out; the current track 'c' is now at
    // index 1, not the saved index 2. The old positional logic — min(2, len-1) —
    // would land on 'd' (index 2); locating by id gives the correct 'c'.
    const saved = ['a', 'b', 'c', 'd'];
    expect(resolveResumeStartIndex(saved, 2, ['b', 'c', 'd'])).toBe(1);
  });

  it('finds the current track regardless of position (shuffled play order)', () => {
    // Saved in play order: current is 'z' at index 0. Even if the valid list is
    // in a different order, we locate 'z' by id.
    const saved = ['z', 'y', 'x'];
    expect(resolveResumeStartIndex(saved, 0, ['x', 'z', 'y'])).toBe(1);
  });

  it('falls back to a clamped index when the current track was dropped', () => {
    // The current track 'c' is gone; fall back to min(savedIndex, len-1).
    const saved = ['a', 'b', 'c'];
    expect(resolveResumeStartIndex(saved, 2, ['a', 'b'])).toBe(1);
  });

  it('falls back to the first valid track for pre-fix rows with an unfindable id', () => {
    const saved = ['a', 'b'];
    expect(resolveResumeStartIndex(saved, 0, ['x', 'y'])).toBe(0);
  });

  it('returns 0 for an empty valid list', () => {
    expect(resolveResumeStartIndex(['a'], 0, [])).toBe(0);
  });
});

describe('reconstructPlayOrder', () => {
  it('rebuilds a shuffled permutation over the natural order', () => {
    // natural [a,b,c,d]; playing shuffled [c,a,d,b], current is 'a'.
    const { playOrder, currentIndex } = reconstructPlayOrder(
      ['a', 'b', 'c', 'd'],
      ['c', 'a', 'd', 'b'],
      'a',
    );
    // c→2, a→0, d→3, b→1
    expect(playOrder).toEqual([2, 0, 3, 1]);
    // 'a' sits at play-order position 1
    expect(currentIndex).toBe(1);
  });

  it('produces identity playOrder for an unshuffled queue', () => {
    const { playOrder, currentIndex } = reconstructPlayOrder(
      ['a', 'b', 'c'],
      ['a', 'b', 'c'],
      'b',
    );
    expect(playOrder).toEqual([0, 1, 2]);
    expect(currentIndex).toBe(1);
  });

  it('remaps around dropped tracks (both lists pre-filtered)', () => {
    // 'b' was dropped from both lists; the permutation still maps correctly.
    const { playOrder, currentIndex } = reconstructPlayOrder(
      ['a', 'c', 'd'],
      ['c', 'a', 'd'],
      'a',
    );
    // c→1, a→0, d→2
    expect(playOrder).toEqual([1, 0, 2]);
    expect(currentIndex).toBe(1);
  });

  it('skips play ids missing from the natural order (defensive)', () => {
    const { playOrder, currentIndex } = reconstructPlayOrder(['a', 'b'], ['a', 'x', 'b'], 'b');
    expect(playOrder).toEqual([0, 1]);
    expect(currentIndex).toBe(1);
  });

  it('falls back to currentIndex 0 when the current track is gone', () => {
    const { playOrder, currentIndex } = reconstructPlayOrder(['a', 'b'], ['a', 'b'], 'z');
    expect(playOrder).toEqual([0, 1]);
    expect(currentIndex).toBe(0);
  });
});
