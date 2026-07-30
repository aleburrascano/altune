import { useQueueStore } from '../queueStore';
import type { PlaybackTrack, QueueSource } from '../types';

const INITIAL_STATE = useQueueStore.getState();

function track(id: string): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: id },
    title: `Track ${id}`,
    artist: 'Artist',
    artworkUrl: null,
  };
}

const PLAYLIST_SOURCE: QueueSource = { kind: 'playlist', playlistId: 'p1', name: 'Chill' };

beforeEach(() => {
  useQueueStore.setState(INITIAL_STATE, true);
});

describe('loadQueue', () => {
  it('builds an identity play order, in order, and lands currentIndex/shuffled/source/resumePosition', () => {
    const tracks = [track('a'), track('b'), track('c')];

    useQueueStore.getState().loadQueue(tracks, 1, PLAYLIST_SOURCE);

    const state = useQueueStore.getState();
    expect(state.playOrder).toEqual([0, 1, 2]);
    expect(state.tracks).toEqual(tracks);
    expect(state.currentIndex).toBe(1);
    expect(state.shuffled).toBe(false);
    expect(state.source).toEqual(PLAYLIST_SOURCE);
    expect(state.resumePositionMs).toBe(0);
  });

  it('resets a previously set resumePositionMs back to 0', () => {
    useQueueStore.getState().setResumePosition(45000);

    useQueueStore.getState().loadQueue([track('a')], 0, null);

    expect(useQueueStore.getState().resumePositionMs).toBe(0);
  });

  describe('with an empty tracks array', () => {
    it('lands on an empty playOrder and an empty tracks list regardless of startIndex', () => {
      useQueueStore.getState().loadQueue([], 3, PLAYLIST_SOURCE);

      const state = useQueueStore.getState();
      expect(state.tracks).toEqual([]);
      expect(state.playOrder).toEqual([]);
      expect(state.source).toEqual(PLAYLIST_SOURCE);
      expect(state.resumePositionMs).toBe(0);
    });

    it("clamps currentIndex to -1, matching restoreQueue's empty-queue contract", () => {
      useQueueStore.getState().loadQueue([], 0, null);

      expect(useQueueStore.getState().currentIndex).toBe(-1);
    });
  });
});

describe('restoreQueue', () => {
  it('takes the given playOrder permutation and shuffled flag verbatim instead of forcing identity order', () => {
    const tracks = [track('a'), track('b'), track('c')];

    useQueueStore.getState().restoreQueue(tracks, [2, 0, 1], 1, PLAYLIST_SOURCE, true);

    const state = useQueueStore.getState();
    expect(state.playOrder).toEqual([2, 0, 1]);
    expect(state.shuffled).toBe(true);
    expect(state.currentIndex).toBe(1);
    expect(state.source).toEqual(PLAYLIST_SOURCE);
    expect(state.resumePositionMs).toBe(0);
  });

  it('clamps a currentIndex past the end of playOrder to the last valid position', () => {
    const tracks = [track('a'), track('b'), track('c')];

    useQueueStore.getState().restoreQueue(tracks, [0, 1, 2], 99, null, false);

    expect(useQueueStore.getState().currentIndex).toBe(2);
  });

  it('clamps a negative currentIndex to 0', () => {
    const tracks = [track('a'), track('b'), track('c')];

    useQueueStore.getState().restoreQueue(tracks, [0, 1, 2], -4, null, false);

    expect(useQueueStore.getState().currentIndex).toBe(0);
  });

  it('forces currentIndex to -1 for an empty playOrder, regardless of the passed currentIndex', () => {
    const tracks = [track('a'), track('b'), track('c')];

    useQueueStore.getState().restoreQueue(tracks, [], 2, null, false);

    const state = useQueueStore.getState();
    expect(state.playOrder).toEqual([]);
    expect(state.currentIndex).toBe(-1);
  });

  it('keeps a playOrder entry pointing at a track deleted between save and restore, and resolves it to no current track', () => {
    const tracks = [track('a'), track('b'), track('c')];

    useQueueStore.getState().restoreQueue(tracks, [0, 3, 1, 2], 1, null, false);

    const state = useQueueStore.getState();
    expect(state.playOrder).toEqual([0, 3, 1, 2]);
    expect(state.currentIndex).toBe(1);
    expect(state.currentTrack()).toBeNull();
  });
});

describe('generation', () => {
  it.each<[string, () => void]>([
    ['loadQueue', () => useQueueStore.getState().loadQueue([track('a')], 0, null)],
    [
      'restoreQueue',
      () => useQueueStore.getState().restoreQueue([track('a')], [0], 0, null, false),
    ],
    ['clearQueue', () => useQueueStore.getState().clearQueue()],
  ])('%s bumps generation when it replaces the queue', (_name, replaceQueue) => {
    useQueueStore.getState().loadQueue([track('a'), track('b')], 0, null);
    const { generation } = useQueueStore.getState();

    replaceQueue();

    expect(useQueueStore.getState().generation).toBe(generation + 1);
  });

  it.each<[string, () => void]>([
    ['skipToNext', () => useQueueStore.getState().skipToNext()],
    ['skipToPrevious', () => useQueueStore.getState().skipToPrevious()],
    ['skipToIndex', () => useQueueStore.getState().skipToIndex(1)],
    ['enqueue', () => useQueueStore.getState().enqueue(track('extra'))],
    ['toggleShuffle', () => useQueueStore.getState().toggleShuffle()],
    ['reorderQueue', () => useQueueStore.getState().reorderQueue(0, 2)],
  ])('%s does not bump generation', (_name, action) => {
    useQueueStore.getState().loadQueue([track('a'), track('b'), track('c')], 1, null);
    const { generation } = useQueueStore.getState();

    action();

    expect(useQueueStore.getState().generation).toBe(generation);
  });

  it('never rewinds when removeFromQueue empties the queue', () => {
    useQueueStore.getState().loadQueue([track('a')], 0, null);
    const { generation } = useQueueStore.getState();

    useQueueStore.getState().removeFromQueue(0);

    const state = useQueueStore.getState();
    expect(state.tracks).toEqual([]);
    expect(state.generation).toBe(generation + 1);
  });

  it('strictly increases across repeated queue replacements, never rewinding', () => {
    const generations = [useQueueStore.getState().generation];

    useQueueStore.getState().loadQueue([track('a')], 0, null);
    generations.push(useQueueStore.getState().generation);

    useQueueStore.getState().clearQueue();
    generations.push(useQueueStore.getState().generation);

    useQueueStore.getState().restoreQueue([track('a'), track('b')], [1, 0], 0, null, true);
    generations.push(useQueueStore.getState().generation);

    for (let i = 1; i < generations.length; i++) {
      expect(generations[i]).toBeGreaterThan(generations[i - 1]!);
    }
  });
});

describe('setResumePosition', () => {
  const seeds: [string, () => void][] = [
    ['an empty queue', () => {}],
    ['a single-track queue', () => useQueueStore.getState().loadQueue([track('a')], 0, null)],
    [
      'a multi-track shuffled queue',
      () =>
        useQueueStore
          .getState()
          .restoreQueue([track('a'), track('b'), track('c')], [2, 0, 1], 1, null, true),
    ],
  ];

  it.each(seeds)('clamps a negative position to 0 over %s', (_label, seed) => {
    seed();

    useQueueStore.getState().setResumePosition(-500);

    expect(useQueueStore.getState().resumePositionMs).toBe(0);
  });

  it('accepts a non-negative position as-is', () => {
    useQueueStore.getState().setResumePosition(45000);

    expect(useQueueStore.getState().resumePositionMs).toBe(45000);
  });

  it('applying the same position twice is idempotent', () => {
    useQueueStore.getState().setResumePosition(12000);
    const once = useQueueStore.getState().resumePositionMs;

    useQueueStore.getState().setResumePosition(12000);

    expect(useQueueStore.getState().resumePositionMs).toBe(once);
  });
});

describe('setShuffled', () => {
  const seeds: [string, () => void, readonly number[]][] = [
    ['an empty queue', () => {}, []],
    ['a single-track queue', () => useQueueStore.getState().loadQueue([track('a')], 0, null), [0]],
    [
      'a multi-track shuffled queue',
      () =>
        useQueueStore
          .getState()
          .restoreQueue([track('a'), track('b'), track('c')], [2, 0, 1], 1, null, true),
      [2, 0, 1],
    ],
  ];

  it.each(seeds)('flips the flag without touching playOrder, over %s', (_label, seed, expectedOrder) => {
    seed();
    const before = useQueueStore.getState().shuffled;

    useQueueStore.getState().setShuffled(!before);

    expect(useQueueStore.getState().shuffled).toBe(!before);
    expect(useQueueStore.getState().playOrder).toEqual(expectedOrder);
  });

  it('setting the same value twice is idempotent', () => {
    useQueueStore
      .getState()
      .restoreQueue([track('a'), track('b'), track('c')], [2, 0, 1], 1, null, false);

    useQueueStore.getState().setShuffled(true);
    const once = useQueueStore.getState();

    useQueueStore.getState().setShuffled(true);
    const twice = useQueueStore.getState();

    expect(once.shuffled).toBe(true);
    expect(twice.shuffled).toBe(true);
    expect(twice.playOrder).toEqual(once.playOrder);
  });
});

describe('clearQueue', () => {
  it.each<[string, () => void]>([
    ['an empty queue', () => useQueueStore.getState().setResumePosition(9000)],
    [
      'a single-track queue',
      () => {
        useQueueStore.getState().loadQueue([track('a')], 0, PLAYLIST_SOURCE);
        useQueueStore.getState().setResumePosition(5000);
      },
    ],
    [
      'a multi-track shuffled queue',
      () => {
        useQueueStore
          .getState()
          .restoreQueue([track('a'), track('b'), track('c')], [2, 0, 1], 1, PLAYLIST_SOURCE, true);
        useQueueStore.getState().setResumePosition(3000);
      },
    ],
  ])('always lands on the initial empty state, over %s', (_label, seed) => {
    seed();

    useQueueStore.getState().clearQueue();

    const state = useQueueStore.getState();
    expect(state.tracks).toEqual([]);
    expect(state.playOrder).toEqual([]);
    expect(state.currentIndex).toBe(-1);
    expect(state.shuffled).toBe(false);
    expect(state.source).toBeNull();
    expect(state.resumePositionMs).toBe(0);
  });

  it('calling clearQueue twice in a row yields the same state, except generation, which still advances', () => {
    useQueueStore.getState().clearQueue();
    const first = useQueueStore.getState();

    useQueueStore.getState().clearQueue();
    const second = useQueueStore.getState();

    expect(second.tracks).toEqual(first.tracks);
    expect(second.playOrder).toEqual(first.playOrder);
    expect(second.currentIndex).toBe(first.currentIndex);
    expect(second.shuffled).toBe(first.shuffled);
    expect(second.source).toBe(first.source);
    expect(second.resumePositionMs).toBe(first.resumePositionMs);
    expect(second.generation).toBe(first.generation + 1);
  });
});
