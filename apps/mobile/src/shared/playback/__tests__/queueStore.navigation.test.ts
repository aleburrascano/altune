import { useQueueStore } from '../queueStore';
import { trackKey } from '../trackKey';
import type { PlaybackTrack, RepeatMode } from '../types';

const INITIAL_STATE = useQueueStore.getState();

beforeEach(() => {
  useQueueStore.setState(INITIAL_STATE, true);
});

function track(id: string): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: id },
    title: `Track ${id}`,
    artist: 'Test Artist',
    artworkUrl: null,
  };
}

function seed(
  tracks: readonly PlaybackTrack[],
  playOrder: readonly number[],
  currentIndex: number,
  repeatMode: RepeatMode = 'off',
): void {
  useQueueStore.setState({ tracks, playOrder, currentIndex, repeatMode });
}

describe('currentTrack', () => {
  it('returns null for an empty queue', () => {
    seed([], [], -1);

    expect(useQueueStore.getState().currentTrack()).toBeNull();
  });

  it('returns null when currentIndex is -1 even with tracks loaded', () => {
    seed([track('a'), track('b')], [0, 1], -1);

    expect(useQueueStore.getState().currentTrack()).toBeNull();
  });

  it('reads through a non-identity playOrder', () => {
    const a = track('a');
    const b = track('b');
    const c = track('c');
    seed([a, b, c], [2, 0, 1], 1);

    expect(useQueueStore.getState().currentTrack()).toBe(a);
  });
});

describe('hasNext / hasPrevious truth table', () => {
  it.each<[RepeatMode, number, boolean, boolean]>([
    ['off', 0, true, false],
    ['off', 1, true, true],
    ['off', 2, false, true],
    ['all', 0, true, true],
    ['all', 1, true, true],
    ['all', 2, true, true],
    ['one', 0, true, false],
    ['one', 1, true, true],
    ['one', 2, false, true],
  ])('repeatMode=%s at index %i -> hasNext=%s hasPrevious=%s', (repeatMode, index, next, previous) => {
    seed([track('a'), track('b'), track('c')], [0, 1, 2], index, repeatMode);

    expect(useQueueStore.getState().hasNext()).toBe(next);
    expect(useQueueStore.getState().hasPrevious()).toBe(previous);
  });

  it.each<RepeatMode>(['off', 'all', 'one'])(
    'is false for an empty queue regardless of repeatMode=%s',
    (repeatMode) => {
      seed([], [], -1, repeatMode);

      expect(useQueueStore.getState().hasNext()).toBe(false);
      expect(useQueueStore.getState().hasPrevious()).toBe(false);
    },
  );
});

describe('skipToNext', () => {
  it.each<[RepeatMode, string, number, string | null, number]>([
    ['off', 'first', 0, 'b', 1],
    ['off', 'middle', 1, 'c', 2],
    ['off', 'last', 2, null, 2],
    ['all', 'first', 0, 'b', 1],
    ['all', 'middle', 1, 'c', 2],
    ['all', 'last', 2, 'a', 0],
    ['one', 'first', 0, 'b', 1],
    ['one', 'middle', 1, 'c', 2],
    ['one', 'last', 2, null, 2],
  ])(
    'repeatMode=%s, %s position (starting index %s): returns %s and lands on index %i',
    (repeatMode, _label, startIndex, expectedId, expectedIndex) => {
      const a = track('a');
      const b = track('b');
      const c = track('c');
      seed([a, b, c], [0, 1, 2], startIndex, repeatMode);

      const result = useQueueStore.getState().skipToNext();

      if (expectedId === null) {
        expect(result).toBeNull();
      } else {
        expect(result && trackKey(result)).toBe(trackKey(track(expectedId)));
      }
      expect(useQueueStore.getState().currentIndex).toBe(expectedIndex);
    },
  );

  it('returns null and does not touch currentIndex for an empty queue', () => {
    seed([], [], -1, 'all');

    expect(useQueueStore.getState().skipToNext()).toBeNull();
    expect(useQueueStore.getState().currentIndex).toBe(-1);
  });
});

describe('skipToPrevious', () => {
  it.each<[RepeatMode, string, number, string, number]>([
    ['off', 'last', 2, 'b', 1],
    ['off', 'middle (currentIndex=1, the prev >= 0 vs prev > 0 boundary)', 1, 'a', 0],
    ['off', 'first', 0, 'a', 0],
    ['all', 'last', 2, 'b', 1],
    ['all', 'middle (currentIndex=1, the prev >= 0 vs prev > 0 boundary)', 1, 'a', 0],
    ['all', 'first', 0, 'c', 2],
    ['one', 'last', 2, 'b', 1],
    ['one', 'middle (currentIndex=1, the prev >= 0 vs prev > 0 boundary)', 1, 'a', 0],
    ['one', 'first', 0, 'a', 0],
  ])(
    'repeatMode=%s, %s (starting index %s): returns %s and lands on index %i',
    (repeatMode, _label, startIndex, expectedId, expectedIndex) => {
      const a = track('a');
      const b = track('b');
      const c = track('c');
      seed([a, b, c], [0, 1, 2], startIndex, repeatMode);

      const result = useQueueStore.getState().skipToPrevious();

      expect(result && trackKey(result)).toBe(trackKey(track(expectedId)));
      expect(useQueueStore.getState().currentIndex).toBe(expectedIndex);
    },
  );

  it('returns null and does not touch currentIndex for an empty queue', () => {
    seed([], [], -1, 'all');

    expect(useQueueStore.getState().skipToPrevious()).toBeNull();
    expect(useQueueStore.getState().currentIndex).toBe(-1);
  });
});

describe('skipToIndex', () => {
  it('moves to an in-range index and returns its track', () => {
    const a = track('a');
    const b = track('b');
    const c = track('c');
    seed([a, b, c], [0, 1, 2], 0);

    const result = useQueueStore.getState().skipToIndex(2);

    expect(result).toBe(c);
    expect(useQueueStore.getState().currentIndex).toBe(2);
  });

  it('rejects a negative index, leaving currentIndex unchanged', () => {
    seed([track('a'), track('b')], [0, 1], 1);

    const result = useQueueStore.getState().skipToIndex(-1);

    expect(result).toBeNull();
    expect(useQueueStore.getState().currentIndex).toBe(1);
  });

  it('rejects an index past the end, leaving currentIndex unchanged', () => {
    seed([track('a'), track('b')], [0, 1], 1);

    const result = useQueueStore.getState().skipToIndex(2);

    expect(result).toBeNull();
    expect(useQueueStore.getState().currentIndex).toBe(1);
  });

  it('returns null for an empty queue', () => {
    seed([], [], -1);

    expect(useQueueStore.getState().skipToIndex(0)).toBeNull();
  });
});

describe('syncCurrentIndex', () => {
  function seedReconciliationQueue(currentIndex: number): {
    a: PlaybackTrack;
    b: PlaybackTrack;
    c: PlaybackTrack;
    d: PlaybackTrack;
  } {
    const a = track('a');
    const b = track('b');
    const c = track('c');
    const d = track('d');
    seed([a, b, c, d], [2, 0, 3, 1], currentIndex);
    return { a, b, c, d };
  }

  it('accepts the reported index when no key is given', () => {
    const { d } = seedReconciliationQueue(0);

    useQueueStore.getState().syncCurrentIndex(2);

    expect(useQueueStore.getState().currentIndex).toBe(2);
    expect(useQueueStore.getState().currentTrack()).toBe(d);
  });

  it('lets the key rescue an index the native player reported out of range', () => {
    const { c } = seedReconciliationQueue(1);

    useQueueStore.getState().syncCurrentIndex(10, trackKey(c));

    expect(useQueueStore.getState().currentIndex).toBe(0);
    expect(useQueueStore.getState().currentTrack()).toBe(c);
  });

  it('resolves the key across a play order still referencing a deleted Track', () => {
    const a = track('a');
    const b = track('b');
    seed([a, b], [0, 5, 1], 0);

    useQueueStore.getState().syncCurrentIndex(1, trackKey(b));

    expect(useQueueStore.getState().currentIndex).toBe(2);
    expect(useQueueStore.getState().currentTrack()).toBe(b);
  });

  it('accepts a reported index of exactly 0 when no key is given', () => {
    const { c } = seedReconciliationQueue(2);

    useQueueStore.getState().syncCurrentIndex(0);

    expect(useQueueStore.getState().currentIndex).toBe(0);
    expect(useQueueStore.getState().currentTrack()).toBe(c);
  });

  it('rejects a reported index of exactly the play-order length when no key is given', () => {
    seedReconciliationQueue(1);
    const before = useQueueStore.getState();

    useQueueStore.getState().syncCurrentIndex(before.playOrder.length);

    expect(useQueueStore.getState()).toBe(before);
    expect(useQueueStore.getState().currentIndex).toBe(1);
  });

  it('rejects an index past the end of the play order when no key is given', () => {
    seedReconciliationQueue(1);
    const before = useQueueStore.getState();

    useQueueStore.getState().syncCurrentIndex(10);

    expect(useQueueStore.getState()).toBe(before);
    expect(useQueueStore.getState().currentIndex).toBe(1);
  });

  it('rejects a negative index when no key is given', () => {
    seedReconciliationQueue(1);
    const before = useQueueStore.getState();

    useQueueStore.getState().syncCurrentIndex(-1);

    expect(useQueueStore.getState()).toBe(before);
    expect(useQueueStore.getState().currentIndex).toBe(1);
  });

  it('the key wins when the reported index points at a different Track', () => {
    const { c } = seedReconciliationQueue(3);

    useQueueStore.getState().syncCurrentIndex(1, trackKey(c));

    expect(useQueueStore.getState().currentIndex).toBe(0);
    expect(useQueueStore.getState().currentTrack()).toBe(c);
  });

  it('resolves the key through the play order, not the raw tracks array index', () => {
    const { b } = seedReconciliationQueue(0);

    useQueueStore.getState().syncCurrentIndex(3, trackKey(b));

    expect(useQueueStore.getState().currentIndex).toBe(3);
    expect(useQueueStore.getState().currentTrack()).toBe(b);
  });

  it('an unknown key moves nothing', () => {
    seedReconciliationQueue(2);
    const before = useQueueStore.getState();

    useQueueStore.getState().syncCurrentIndex(0, 'library:does-not-exist');

    expect(useQueueStore.getState()).toBe(before);
    expect(useQueueStore.getState().currentIndex).toBe(2);
  });

  describe('a Track duplicated in the queue', () => {
    it('picks the occurrence nearest the reported index when there is no tie', () => {
      const x = track('dup');
      const y = track('y');
      const z = track('z');
      const x2 = track('dup');
      seed([x, y, z, x2], [0, 1, 2, 3], 0);

      useQueueStore.getState().syncCurrentIndex(2, trackKey(x));

      expect(useQueueStore.getState().currentIndex).toBe(3);
      expect(trackKey(useQueueStore.getState().currentTrack()!)).toBe(trackKey(x));
    });

    it('breaks a tie in favor of the earlier occurrence', () => {
      const x = track('dup');
      const y = track('y');
      const x2 = track('dup');
      const z = track('z');
      seed([x, y, x2, z], [0, 1, 2, 3], 3);

      useQueueStore.getState().syncCurrentIndex(1, trackKey(x));

      expect(useQueueStore.getState().currentIndex).toBe(0);
    });
  });

  describe('replay', () => {
    it('a repeated plain-index report does not move the cursor again', () => {
      seedReconciliationQueue(0);
      useQueueStore.getState().syncCurrentIndex(2);
      const once = useQueueStore.getState();

      useQueueStore.getState().syncCurrentIndex(2);

      expect(useQueueStore.getState()).toBe(once);
      expect(useQueueStore.getState().currentIndex).toBe(2);
    });

    it('a repeated key-resolved report does not move the cursor again', () => {
      const { c } = seedReconciliationQueue(3);
      useQueueStore.getState().syncCurrentIndex(1, trackKey(c));
      const once = useQueueStore.getState();

      useQueueStore.getState().syncCurrentIndex(1, trackKey(c));

      expect(useQueueStore.getState()).toBe(once);
      expect(useQueueStore.getState().currentIndex).toBe(0);
    });

    it('a repeated rejected (unknown key) report stays a no-op', () => {
      seedReconciliationQueue(2);
      useQueueStore.getState().syncCurrentIndex(0, 'library:does-not-exist');
      const once = useQueueStore.getState();

      useQueueStore.getState().syncCurrentIndex(0, 'library:does-not-exist');

      expect(useQueueStore.getState()).toBe(once);
      expect(useQueueStore.getState().currentIndex).toBe(2);
    });
  });
});
