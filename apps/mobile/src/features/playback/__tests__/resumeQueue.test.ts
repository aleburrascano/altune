import {
  currentOccurrence,
  currentTrackId,
  reconstructPlayOrder,
  resolveResumeStartIndex,
} from '../resumeQueue';

describe('currentTrackId — resolving the saved cursor to an id', () => {
  it('returns the id at the saved index when it is in range', () => {
    expect(currentTrackId(['a', 'b', 'c'], 1)).toBe('b');
  });

  it('falls back to the first id when the saved index is out of range', () => {
    expect(currentTrackId(['a', 'b', 'c'], 9)).toBe('a');
  });

  it('falls back to the first id when the saved index is negative', () => {
    expect(currentTrackId(['a', 'b', 'c'], -1)).toBe('a');
  });

  it('returns the empty string when there are no saved ids', () => {
    expect(currentTrackId([], 0)).toBe('');
  });
});

describe('currentOccurrence — which copy of a duplicated id the saved cursor points at', () => {
  it('is 0 for the first copy', () => {
    expect(currentOccurrence(['a', 'b', 'a'], 0)).toBe(0);
  });

  it('counts the earlier copies of the same id', () => {
    expect(currentOccurrence(['a', 'b', 'a', 'a'], 3)).toBe(2);
  });

  it('is 0 when the saved index is out of range (the cursor falls back to the first id)', () => {
    expect(currentOccurrence(['a', 'b', 'a'], 7)).toBe(0);
    expect(currentOccurrence([], 0)).toBe(0);
  });
});

describe('resolveResumeStartIndex — where playback resumes against the currently valid ids', () => {
  it('is 0 when no valid ids remain', () => {
    expect(resolveResumeStartIndex(['a', 'b'], 1, [])).toBe(0);
  });

  it('follows the saved track by identity, not by its old position', () => {
    expect(resolveResumeStartIndex(['a', 'b', 'c'], 2, ['x', 'c', 'y'])).toBe(1);
  });

  it('returns index 0 when the saved track is now the first valid id', () => {
    expect(resolveResumeStartIndex(['a', 'b', 'c'], 2, ['c', 'x', 'y'])).toBe(0);
  });

  it('clamps to the saved index when no id was ever saved, not to the first valid track', () => {
    expect(resolveResumeStartIndex([], 2, ['x', 'y', 'z', 'w'])).toBe(2);
  });

  it('clamps to the last valid index when the saved track is gone and the index overshoots', () => {
    expect(resolveResumeStartIndex(['a', 'b', 'c'], 2, ['x', 'y'])).toBe(1);
  });

  it('clamps a negative saved index to 0 when the saved track is gone', () => {
    expect(resolveResumeStartIndex(['a'], -3, ['x', 'y'])).toBe(0);
  });

  it('lands on the second copy of a duplicated track when the second copy was playing', () => {
    expect(resolveResumeStartIndex(['a', 'b', 'a', 'c'], 2, ['a', 'b', 'a', 'c'])).toBe(2);
  });

  it('keeps the playing copy when an unavailable track before it was dropped', () => {
    expect(resolveResumeStartIndex(['a', 'b', 'a', 'c'], 2, ['a', 'a', 'c'])).toBe(1);
  });

  it('falls back to the last copy when fewer copies remain than the saved occurrence', () => {
    expect(resolveResumeStartIndex(['a', 'a', 'a'], 2, ['x', 'a', 'y', 'a'])).toBe(3);
  });
});

describe('reconstructPlayOrder — rebuilding the play order over the currently present tracks', () => {
  it('maps play ids to their positions in the natural order', () => {
    const { playOrder, currentIndex } = reconstructPlayOrder(
      ['a', 'b', 'c'],
      ['c', 'a', 'b'],
      'a',
      0,
    );

    expect(playOrder).toEqual([2, 0, 1]);
    expect(currentIndex).toBe(1);
  });

  it('drops play ids that are no longer present in the natural order', () => {
    const { playOrder, currentIndex } = reconstructPlayOrder(
      ['a', 'b'],
      ['a', 'ghost', 'b'],
      'b',
      0,
    );

    expect(playOrder).toEqual([0, 1]);
    expect(currentIndex).toBe(1);
  });

  it('keeps currentIndex at 0 when the current id is absent from the play ids', () => {
    const { playOrder, currentIndex } = reconstructPlayOrder(['a', 'b'], ['b', 'a'], 'missing', 0);

    expect(playOrder).toEqual([1, 0]);
    expect(currentIndex).toBe(0);
  });

  it('lands on the second copy of a duplicated track when the second copy was playing', () => {
    const { playOrder, currentIndex } = reconstructPlayOrder(
      ['a', 'b', 'a', 'c'],
      ['a', 'b', 'a', 'c'],
      'a',
      1,
    );

    expect(playOrder).toEqual([0, 1, 2, 3]);
    expect(currentIndex).toBe(2);
  });

  it('stays on the first copy of a duplicated track when the first copy was playing', () => {
    const { currentIndex } = reconstructPlayOrder(['a', 'b', 'a'], ['a', 'b', 'a'], 'a', 0);

    expect(currentIndex).toBe(0);
  });

  it('maps each shuffled copy of a duplicated id to a distinct natural track', () => {
    const { playOrder, currentIndex } = reconstructPlayOrder(
      ['a', 'b', 'a'],
      ['a', 'a', 'b'],
      'a',
      1,
    );

    expect([...playOrder].sort()).toEqual([0, 1, 2]);
    expect(playOrder).toEqual([0, 2, 1]);
    expect(currentIndex).toBe(1);
  });

  it('falls back to the last copy when the play order holds fewer copies than the occurrence', () => {
    const { playOrder, currentIndex } = reconstructPlayOrder(['a', 'b'], ['b', 'a'], 'a', 3);

    expect(playOrder).toEqual([1, 0]);
    expect(currentIndex).toBe(1);
  });

  it('reuses the last natural copy when the play order holds more copies than the natural order', () => {
    const { playOrder } = reconstructPlayOrder(['a', 'b'], ['a', 'b', 'a'], 'b', 0);

    expect(playOrder).toEqual([0, 1, 0]);
  });
});
