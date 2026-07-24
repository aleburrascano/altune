import { reconstructPlayOrder, resolveResumeStartIndex } from '../resumeQueue';

describe('resolveResumeStartIndex', () => {
  it('returns the saved index when nothing was filtered out', () => {
    const saved = ['a', 'b', 'c'];
    expect(resolveResumeStartIndex(saved, 2, ['a', 'b', 'c'])).toBe(2);
  });

  it('relocates the current track when an earlier track was dropped (filter shift)', () => {
    const saved = ['a', 'b', 'c', 'd'];
    expect(resolveResumeStartIndex(saved, 2, ['b', 'c', 'd'])).toBe(1);
  });

  it('finds the current track regardless of position (shuffled play order)', () => {
    const saved = ['z', 'y', 'x'];
    expect(resolveResumeStartIndex(saved, 0, ['x', 'z', 'y'])).toBe(1);
  });

  it('falls back to a clamped index when the current track was dropped', () => {
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
    const { playOrder, currentIndex } = reconstructPlayOrder(
      ['a', 'b', 'c', 'd'],
      ['c', 'a', 'd', 'b'],
      'a',
    );
    expect(playOrder).toEqual([2, 0, 3, 1]);
    expect(currentIndex).toBe(1);
  });

  it('produces identity playOrder for an unshuffled queue', () => {
    const { playOrder, currentIndex } = reconstructPlayOrder(['a', 'b', 'c'], ['a', 'b', 'c'], 'b');
    expect(playOrder).toEqual([0, 1, 2]);
    expect(currentIndex).toBe(1);
  });

  it('remaps around dropped tracks (both lists pre-filtered)', () => {
    const { playOrder, currentIndex } = reconstructPlayOrder(['a', 'c', 'd'], ['c', 'a', 'd'], 'a');
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
