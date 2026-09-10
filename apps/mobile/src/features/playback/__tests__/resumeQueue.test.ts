import {
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
});

describe('reconstructPlayOrder — rebuilding the play order over the currently present tracks', () => {
  it('maps play ids to their positions in the natural order', () => {
    const { playOrder, currentIndex } = reconstructPlayOrder(
      ['a', 'b', 'c'],
      ['c', 'a', 'b'],
      'a',
    );

    expect(playOrder).toEqual([2, 0, 1]);
    expect(currentIndex).toBe(1);
  });

  it('drops play ids that are no longer present in the natural order', () => {
    const { playOrder, currentIndex } = reconstructPlayOrder(
      ['a', 'b'],
      ['a', 'ghost', 'b'],
      'b',
    );

    expect(playOrder).toEqual([0, 1]);
    expect(currentIndex).toBe(1);
  });

  it('keeps currentIndex at 0 when the current id is absent from the play ids', () => {
    const { playOrder, currentIndex } = reconstructPlayOrder(['a', 'b'], ['b', 'a'], 'missing');

    expect(playOrder).toEqual([1, 0]);
    expect(currentIndex).toBe(0);
  });
});
