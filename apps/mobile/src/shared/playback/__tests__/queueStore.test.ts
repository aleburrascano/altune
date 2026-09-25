import fc from 'fast-check';

import { asTrackId, asPlaylistId } from '@shared/api-client/ids';

import { orderedQueueTracks, useQueueStore } from '../queueStore';
import { trackKey } from '../trackKey';
import type { PlaybackTrack, QueueSource, RepeatMode } from '../types';

const INITIAL_STATE = useQueueStore.getState();

function track(id: string): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: asTrackId(id) },
    title: `Track ${id}`,
    artist: 'Test Artist',
    artworkUrl: null,
  };
}

function loadFive() {
  useQueueStore
    .getState()
    .loadQueue([track('a'), track('b'), track('c'), track('d'), track('e')], 2, null);
}

beforeEach(() => {
  useQueueStore.setState(INITIAL_STATE, true);
});

afterEach(() => {
  jest.restoreAllMocks();
});

describe('reorderQueue', () => {
  it('leaves currentIndex untouched when the move does not cross it (identity arm)', () => {
    loadFive();

    useQueueStore.getState().reorderQueue(0, 1);

    const state = useQueueStore.getState();
    expect(state.playOrder).toEqual([1, 0, 2, 3, 4]);
    expect(state.currentIndex).toBe(2);
  });

  it('follows the playing Track to toIndex when fromIndex is the currently playing slot', () => {
    loadFive();

    useQueueStore.getState().reorderQueue(2, 4);

    const state = useQueueStore.getState();
    expect(state.playOrder).toEqual([0, 1, 3, 4, 2]);
    expect(state.currentIndex).toBe(4);
  });

  it('shifts currentIndex left when an earlier item moves to or past it', () => {
    loadFive();

    useQueueStore.getState().reorderQueue(0, 3);

    const state = useQueueStore.getState();
    expect(state.playOrder).toEqual([1, 2, 3, 0, 4]);
    expect(state.currentIndex).toBe(1);
  });

  it('shifts currentIndex left when an earlier item lands exactly on the playing slot', () => {
    loadFive();
    const playing = useQueueStore.getState().currentTrack();

    useQueueStore.getState().reorderQueue(0, 2);

    const state = useQueueStore.getState();
    expect(state.playOrder).toEqual([1, 2, 0, 3, 4]);
    expect(state.currentIndex).toBe(1);
    expect(state.currentTrack()).toBe(playing);
  });

  it('shifts currentIndex right when a later item moves to or before it', () => {
    loadFive();

    useQueueStore.getState().reorderQueue(4, 1);

    const state = useQueueStore.getState();
    expect(state.playOrder).toEqual([0, 4, 1, 2, 3]);
    expect(state.currentIndex).toBe(3);
  });

  it('shifts currentIndex right when a later item lands exactly on the playing slot', () => {
    loadFive();
    const playing = useQueueStore.getState().currentTrack();

    useQueueStore.getState().reorderQueue(4, 2);

    const state = useQueueStore.getState();
    expect(state.playOrder).toEqual([0, 1, 4, 2, 3]);
    expect(state.currentIndex).toBe(3);
    expect(state.currentTrack()).toBe(playing);
  });

  it('is a true no-op when fromIndex equals toIndex', () => {
    loadFive();
    const before = useQueueStore.getState();

    useQueueStore.getState().reorderQueue(2, 2);

    expect(useQueueStore.getState().playOrder).toBe(before.playOrder);
    expect(useQueueStore.getState().currentIndex).toBe(before.currentIndex);
  });

  it.each<[string, number, number]>([
    ['negative fromIndex', -1, 2],
    ['fromIndex past the end', 5, 2],
    ['negative toIndex', 2, -1],
    ['toIndex past the end', 2, 5],
  ])('rejects an out-of-range move (%s)', (_label, from, to) => {
    loadFive();
    const before = useQueueStore.getState();

    useQueueStore.getState().reorderQueue(from, to);

    expect(useQueueStore.getState().playOrder).toBe(before.playOrder);
    expect(useQueueStore.getState().currentIndex).toBe(before.currentIndex);
  });

  it.each<[string, number, number]>([
    ['NaN fromIndex', Number.NaN, 2],
    ['NaN toIndex', 2, Number.NaN],
    ['fractional fromIndex', 0.5, 2],
    ['fractional toIndex', 2, 0.5],
  ])('rejects a non-integer move (%s), leaving the store state unchanged', (_label, from, to) => {
    loadFive();
    const before = useQueueStore.getState();

    const upcoming = useQueueStore.getState().reorderQueue(from, to);

    expect(useQueueStore.getState()).toBe(before);
    expect(upcoming).toEqual([track('d'), track('e')]);
  });
});

describe('removeFromQueue', () => {
  it('removing a Track before the current position shifts currentIndex down', () => {
    loadFive();

    useQueueStore.getState().removeFromQueue(0);

    const state = useQueueStore.getState();
    expect(state.tracks.map((t) => t.title)).toEqual(['Track b', 'Track c', 'Track d', 'Track e']);
    expect(state.playOrder).toEqual([0, 1, 2, 3]);
    expect(state.currentIndex).toBe(1);
  });

  it('removing the currently playing Track advances current to what is now next', () => {
    loadFive();

    useQueueStore.getState().removeFromQueue(2);

    const state = useQueueStore.getState();
    expect(state.tracks.map((t) => t.title)).toEqual(['Track a', 'Track b', 'Track d', 'Track e']);
    expect(state.playOrder).toEqual([0, 1, 2, 3]);
    expect(state.currentIndex).toBe(2);
    expect(state.currentTrack()?.title).toBe('Track d');
  });

  it('removing a Track after the current position leaves currentIndex untouched', () => {
    loadFive();

    useQueueStore.getState().removeFromQueue(4);

    const state = useQueueStore.getState();
    expect(state.tracks.map((t) => t.title)).toEqual(['Track a', 'Track b', 'Track c', 'Track d']);
    expect(state.playOrder).toEqual([0, 1, 2, 3]);
    expect(state.currentIndex).toBe(2);
  });

  it('removing the last remaining Track resets the queue to empty', () => {
    useQueueStore.getState().loadQueue([track('a')], 0, null);

    useQueueStore.getState().removeFromQueue(0);

    const state = useQueueStore.getState();
    expect(state.tracks).toEqual([]);
    expect(state.playOrder).toEqual([]);
    expect(state.currentIndex).toBe(-1);
    expect(state.shuffled).toBe(false);
  });

  it.each<[string, number]>([
    ['negative index', -1],
    ['index past the end', 5],
  ])('rejects an out-of-range removal (%s)', (_label, index) => {
    loadFive();
    const before = useQueueStore.getState();

    useQueueStore.getState().removeFromQueue(index);

    expect(useQueueStore.getState().tracks).toBe(before.tracks);
    expect(useQueueStore.getState().playOrder).toBe(before.playOrder);
    expect(useQueueStore.getState().currentIndex).toBe(before.currentIndex);
  });

  it('keeps shuffled true when more than one Track remains after removal', () => {
    loadFive();
    jest.spyOn(Math, 'random').mockReturnValue(0);
    useQueueStore.getState().toggleShuffle();

    useQueueStore.getState().removeFromQueue(4);

    expect(useQueueStore.getState().shuffled).toBe(true);
  });

  it('clears shuffled once a removal leaves only one Track', () => {
    useQueueStore.getState().loadQueue([track('a'), track('b')], 0, null);
    jest.spyOn(Math, 'random').mockReturnValue(0);
    useQueueStore.getState().toggleShuffle();

    useQueueStore.getState().removeFromQueue(1);

    expect(useQueueStore.getState().shuffled).toBe(false);
  });
});

describe('clearUpcoming', () => {
  it('keeps the played Tracks and renumbers playOrder back to a permutation over them', () => {
    loadFive();

    useQueueStore.getState().clearUpcoming();

    const state = useQueueStore.getState();
    expect(state.tracks.map((t) => t.title)).toEqual(['Track a', 'Track b', 'Track c']);
    expect(state.playOrder).toEqual([0, 1, 2]);
    expect(state.currentIndex).toBe(2);
    expect(state.currentTrack()?.title).toBe('Track c');
  });

  it('renumbers a shuffled queue against the order it was playing, not the load order', () => {
    loadFive();
    useQueueStore.getState().skipToIndex(1);
    jest.spyOn(Math, 'random').mockReturnValueOnce(0).mockReturnValueOnce(0);
    useQueueStore.getState().toggleShuffle();
    useQueueStore.getState().skipToIndex(2);

    useQueueStore.getState().clearUpcoming();

    const state = useQueueStore.getState();
    expect(state.tracks.map((t) => t.title)).toEqual(['Track a', 'Track b', 'Track d']);
    expect(state.playOrder).toEqual([0, 1, 2]);
  });

  it('is a true no-op when the cursor is already on the last Track', () => {
    loadFive();
    useQueueStore.getState().skipToIndex(4);
    const before = useQueueStore.getState();

    useQueueStore.getState().clearUpcoming();

    expect(useQueueStore.getState()).toBe(before);
  });

  it('empties the queue when nothing is playing yet', () => {
    useQueueStore.getState().loadQueue([track('a'), track('b')], -1, null);

    useQueueStore.getState().clearUpcoming();

    const state = useQueueStore.getState();
    expect(state.tracks).toEqual([]);
    expect(state.playOrder).toEqual([]);
    expect(state.currentIndex).toBe(-1);
  });

  it('clears shuffled once only the playing Track is left', () => {
    useQueueStore.getState().loadQueue([track('a'), track('b')], 0, null);
    jest.spyOn(Math, 'random').mockReturnValue(0);
    useQueueStore.getState().toggleShuffle();

    useQueueStore.getState().clearUpcoming();

    expect(useQueueStore.getState().shuffled).toBe(false);
  });

  it('keeps shuffled on while more than one played Track remains', () => {
    loadFive();
    jest.spyOn(Math, 'random').mockReturnValue(0);
    useQueueStore.getState().toggleShuffle();

    useQueueStore.getState().clearUpcoming();

    expect(useQueueStore.getState().shuffled).toBe(true);
  });
});

describe('toggleShuffle', () => {
  it('is a no-op on an empty queue', () => {
    useQueueStore.getState().toggleShuffle();

    const state = useQueueStore.getState();
    expect(state.playOrder).toEqual([]);
    expect(state.shuffled).toBe(false);
  });

  it('is a no-op on a single-Track queue', () => {
    useQueueStore.getState().loadQueue([track('a')], 0, null);
    const before = useQueueStore.getState().playOrder;

    useQueueStore.getState().toggleShuffle();

    expect(useQueueStore.getState().playOrder).toBe(before);
    expect(useQueueStore.getState().shuffled).toBe(false);
  });

  it('shuffles only the upcoming tail, following the exact Fisher-Yates draws from Math.random', () => {
    loadFive();
    useQueueStore.getState().skipToIndex(1);
    jest.spyOn(Math, 'random').mockReturnValueOnce(0).mockReturnValueOnce(0);

    useQueueStore.getState().toggleShuffle();

    const state = useQueueStore.getState();
    expect(state.playOrder).toEqual([0, 1, 3, 4, 2]);
    expect(state.currentIndex).toBe(1);
    expect(state.shuffled).toBe(true);
  });

  it('un-shuffling sorts the upcoming tail back to natural ascending order', () => {
    loadFive();
    useQueueStore.getState().skipToIndex(1);
    jest.spyOn(Math, 'random').mockReturnValueOnce(0).mockReturnValueOnce(0);
    useQueueStore.getState().toggleShuffle();

    useQueueStore.getState().toggleShuffle();

    const state = useQueueStore.getState();
    expect(state.playOrder).toEqual([0, 1, 2, 3, 4]);
    expect(state.shuffled).toBe(false);
  });

  it('preserves the upcoming Tracks as a multiset and never touches history, for any random draw', () => {
    const tracks = [track('a'), track('b'), track('c'), track('d'), track('e'), track('f')];

    fc.assert(
      fc.property(
        fc.array(fc.double({ min: 0, max: 0.999, noNaN: true }), { minLength: 1, maxLength: 20 }),
        (draws) => {
          useQueueStore.setState(INITIAL_STATE, true);
          useQueueStore.getState().loadQueue(tracks, 2, null);
          let i = 0;
          jest.spyOn(Math, 'random').mockImplementation(() => draws[i++ % draws.length]!);

          const before = useQueueStore.getState().playOrder;
          const head = before.slice(0, 3);
          const tail = before.slice(3);

          useQueueStore.getState().toggleShuffle();

          const after = useQueueStore.getState();
          expect(after.playOrder.slice(0, 3)).toEqual(head);
          expect([...after.playOrder.slice(3)].sort((a, b) => a - b)).toEqual(
            [...tail].sort((a, b) => a - b),
          );

          jest.restoreAllMocks();
        },
      ),
    );
  });
});

describe('enqueue', () => {
  it('adds the first Track to an empty queue', () => {
    useQueueStore.getState().enqueue(track('a'));

    const state = useQueueStore.getState();
    expect(state.tracks.map((t) => t.title)).toEqual(['Track a']);
    expect(state.playOrder).toEqual([0]);
  });

  it('appends to the end of a shuffled queue without disturbing its order', () => {
    useQueueStore.getState().loadQueue([track('a'), track('b'), track('c')], 0, null);
    jest.spyOn(Math, 'random').mockReturnValueOnce(0);
    useQueueStore.getState().toggleShuffle();
    const orderBefore = useQueueStore.getState().playOrder;

    useQueueStore.getState().enqueue(track('d'));

    const state = useQueueStore.getState();
    expect(state.tracks.map((t) => t.title)).toEqual(['Track a', 'Track b', 'Track c', 'Track d']);
    expect(state.playOrder).toEqual([...orderBefore, 3]);
  });

  it('leaves currentIndex untouched when the cursor is on the last Track', () => {
    loadFive();
    useQueueStore.getState().skipToIndex(4);

    useQueueStore.getState().enqueue(track('f'));

    const state = useQueueStore.getState();
    expect(state.playOrder).toEqual([0, 1, 2, 3, 4, 5]);
    expect(state.currentIndex).toBe(4);
  });
});

describe('playNext', () => {
  it('inserts the sole entry when the queue starts empty', () => {
    useQueueStore.getState().playNext(track('a'));

    const state = useQueueStore.getState();
    expect(state.tracks.map((t) => t.title)).toEqual(['Track a']);
    expect(state.playOrder).toEqual([0]);
  });

  it('inserts right after the current position in a shuffled queue', () => {
    useQueueStore.getState().loadQueue([track('a'), track('b'), track('c')], 0, null);
    jest.spyOn(Math, 'random').mockReturnValueOnce(0);
    useQueueStore.getState().toggleShuffle();

    useQueueStore.getState().playNext(track('d'));

    const state = useQueueStore.getState();
    expect(state.tracks.map((t) => t.title)).toEqual(['Track a', 'Track b', 'Track c', 'Track d']);
    expect(state.playOrder[0]).toBe(0);
    expect(state.playOrder[1]).toBe(3);
    expect(state.playOrder).toHaveLength(4);
  });

  it('appends at the end when the cursor is already on the last Track', () => {
    loadFive();
    useQueueStore.getState().skipToIndex(4);

    useQueueStore.getState().playNext(track('f'));

    const state = useQueueStore.getState();
    expect(state.playOrder).toEqual([0, 1, 2, 3, 4, 5]);
    expect(state.currentIndex).toBe(4);
  });
});

describe('cycleRepeatMode', () => {
  it('cycles off -> all -> one -> off in a full round trip', () => {
    expect(useQueueStore.getState().repeatMode).toBe('off');

    useQueueStore.getState().cycleRepeatMode();
    expect(useQueueStore.getState().repeatMode).toBe('all');

    useQueueStore.getState().cycleRepeatMode();
    expect(useQueueStore.getState().repeatMode).toBe('one');

    useQueueStore.getState().cycleRepeatMode();
    expect(useQueueStore.getState().repeatMode).toBe('off');
  });
});

describe('setRepeatMode', () => {
  it.each<['off' | 'all' | 'one']>([['off'], ['all'], ['one']])(
    'sets repeatMode to %s directly',
    (mode) => {
      useQueueStore.getState().setRepeatMode(mode);

      expect(useQueueStore.getState().repeatMode).toBe(mode);
    },
  );
});

describe('orderedQueueTracks', () => {
  it('flattens a non-identity playOrder into the matching Track sequence', () => {
    const tracks = [track('a'), track('b'), track('c')];

    const result = orderedQueueTracks({ tracks, playOrder: [2, 0, 1] });

    expect(result.map((t) => t.title)).toEqual(['Track c', 'Track a', 'Track b']);
  });

  it('drops a playOrder entry that has no matching Track', () => {
    const tracks = [track('a'), track('b'), track('c')];

    const result = orderedQueueTracks({ tracks, playOrder: [0, 7, 2] });

    expect(result.map((t) => t.title)).toEqual(['Track a', 'Track c']);
  });
});

describe('functional: playing Track survives queue edits', () => {
  it('reordering the Queue never moves or restarts the Track currently playing', () => {
    loadFive();
    const playing = useQueueStore.getState().currentTrack();

    useQueueStore.getState().reorderQueue(0, 4);

    expect(useQueueStore.getState().currentTrack()).toBe(playing);
  });

  it('moving the playing Track itself keeps it current instead of restarting a different one', () => {
    loadFive();
    const playing = useQueueStore.getState().currentTrack();

    useQueueStore.getState().reorderQueue(2, 4);

    expect(useQueueStore.getState().currentTrack()).toBe(playing);
  });

  it('shuffling never disturbs the currently playing Track or the history behind it', () => {
    loadFive();
    jest.spyOn(Math, 'random').mockReturnValue(0);
    const before = orderedQueueTracks(useQueueStore.getState());
    const history = before.slice(0, 3);

    useQueueStore.getState().toggleShuffle();

    const after = orderedQueueTracks(useQueueStore.getState());
    expect(after.slice(0, 3)).toEqual(history);
    expect(after.slice(3).map(trackKey).sort()).toEqual(before.slice(3).map(trackKey).sort());
  });

  it('un-shuffling restores the upcoming Tracks to their natural order while history stays exactly as played', () => {
    loadFive();
    const before = orderedQueueTracks(useQueueStore.getState());
    jest.spyOn(Math, 'random').mockReturnValue(0);
    useQueueStore.getState().toggleShuffle();

    useQueueStore.getState().toggleShuffle();

    expect(orderedQueueTracks(useQueueStore.getState())).toEqual(before);
  });
});

describe('mutators return the post-mutation slice the native player needs', () => {
  function upcomingNow(): PlaybackTrack[] {
    const s = useQueueStore.getState();
    return orderedQueueTracks(s).slice(s.currentIndex + 1);
  }

  it('loadQueue returns the ordered queue and the committed currentIndex', () => {
    const view = useQueueStore.getState().loadQueue([track('a'), track('b')], 1, null);

    expect(view).toEqual({ ordered: [track('a'), track('b')], currentIndex: 1 });
  });

  it('loadQueue returns an empty view with currentIndex -1 for an empty list', () => {
    expect(useQueueStore.getState().loadQueue([], 3, null)).toEqual({
      ordered: [],
      currentIndex: -1,
    });
  });

  it('loadShuffled returns the shuffled order exactly as committed to the store', () => {
    const view = useQueueStore
      .getState()
      .loadShuffled([track('a'), track('b'), track('c'), track('d')], null);

    const s = useQueueStore.getState();
    expect(view).toEqual({ ordered: orderedQueueTracks(s), currentIndex: s.currentIndex });
  });

  it('reorderQueue returns the upcoming Tracks after the move', () => {
    loadFive();

    const upcoming = useQueueStore.getState().reorderQueue(4, 3);

    expect(upcoming).toEqual([track('e'), track('d')]);
    expect(upcoming).toEqual(upcomingNow());
  });

  it.each([
    ['same index', 2, 2],
    ['from out of range', 9, 1],
    ['to out of range', 1, 9],
  ])('reorderQueue returns the unchanged upcoming Tracks on a rejected move (%s)', (_, from, to) => {
    loadFive();

    expect(useQueueStore.getState().reorderQueue(from, to)).toEqual([track('d'), track('e')]);
  });

  it('toggleShuffle returns the upcoming Tracks after reshuffling', () => {
    loadFive();
    jest.spyOn(Math, 'random').mockReturnValue(0);

    const upcoming = useQueueStore.getState().toggleShuffle();

    expect(upcoming).toEqual([track('e'), track('d')]);
    expect(upcoming).toEqual(upcomingNow());
  });

  it('toggleShuffle returns the unchanged upcoming Tracks when there is nothing to shuffle', () => {
    useQueueStore.getState().loadQueue([track('a')], 0, null);

    expect(useQueueStore.getState().toggleShuffle()).toEqual([]);
  });
});

describe('lifecycle', () => {
  function track(id: string): PlaybackTrack {
    return {
      source: { kind: 'library', trackId: asTrackId(id) },
      title: `Track ${id}`,
      artist: 'Artist',
      artworkUrl: null,
    };
  }

  const PLAYLIST_SOURCE: QueueSource = {
    kind: 'playlist',
    playlistId: asPlaylistId('p1'),
    name: 'Chill',
  };

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

  describe('loadShuffled', () => {
    it('draws a play order across the entire library, not a loaded prefix, for a >200-track queue', () => {
      const tracks = Array.from({ length: 250 }, (_, i) => track(`t${i}`));

      useQueueStore.getState().loadShuffled(tracks, null);

      const state = useQueueStore.getState();
      // Every one of the 250 tracks is reachable: the order is a full permutation
      // of all indices, so shuffle spans the whole library rather than a page of it.
      expect(state.playOrder).toHaveLength(250);
      expect([...state.playOrder].sort((a, b) => a - b)).toEqual(
        Array.from({ length: 250 }, (_, i) => i),
      );
      expect(state.tracks).toEqual(tracks);
      expect(state.shuffled).toBe(true);
      expect(state.currentIndex).toBe(0);
    });

    it('actually reorders rather than leaving identity order', () => {
      const randomSpy = jest.spyOn(Math, 'random').mockReturnValue(0);
      const tracks = [track('a'), track('b'), track('c'), track('d')];

      useQueueStore.getState().loadShuffled(tracks, PLAYLIST_SOURCE);

      // With Math.random pinned to 0, Fisher-Yates rotates the tail deterministically
      // away from identity, proving the order was shuffled.
      expect(useQueueStore.getState().playOrder).not.toEqual([0, 1, 2, 3]);
      expect(useQueueStore.getState().source).toEqual(PLAYLIST_SOURCE);
      randomSpy.mockRestore();
    });

    it('un-shuffling via toggleShuffle restores the ascending library order', () => {
      const randomSpy = jest.spyOn(Math, 'random').mockReturnValue(0);
      const tracks = [track('a'), track('b'), track('c'), track('d'), track('e')];
      useQueueStore.getState().loadShuffled(tracks, null);

      useQueueStore.getState().toggleShuffle();

      const state = useQueueStore.getState();
      expect(state.shuffled).toBe(false);
      // The head stays fixed (currentIndex 0); the tail returns to ascending order.
      expect(state.playOrder.slice(1)).toEqual([...state.playOrder.slice(1)].sort((a, b) => a - b));
      randomSpy.mockRestore();
    });

    it('lands currentIndex at -1 for an empty library and bumps generation', () => {
      useQueueStore.getState().loadQueue([track('seed')], 0, null);
      const { generation } = useQueueStore.getState();

      useQueueStore.getState().loadShuffled([], null);

      const state = useQueueStore.getState();
      expect(state.tracks).toEqual([]);
      expect(state.playOrder).toEqual([]);
      expect(state.currentIndex).toBe(-1);
      expect(state.generation).toBe(generation + 1);
    });
  });

  describe('restoreQueue', () => {
    it('takes the given playOrder permutation and shuffled flag verbatim instead of forcing identity order', () => {
      const tracks = [track('a'), track('b'), track('c')];

      useQueueStore
        .getState()
        .restoreQueue({
          tracks,
          playOrder: [2, 0, 1],
          currentIndex: 1,
          source: PLAYLIST_SOURCE,
          shuffled: true,
        });

      const state = useQueueStore.getState();
      expect(state.playOrder).toEqual([2, 0, 1]);
      expect(state.shuffled).toBe(true);
      expect(state.currentIndex).toBe(1);
      expect(state.source).toEqual(PLAYLIST_SOURCE);
      expect(state.resumePositionMs).toBe(0);
    });

    it('clamps a currentIndex past the end of playOrder to the last valid position', () => {
      const tracks = [track('a'), track('b'), track('c')];

      useQueueStore
        .getState()
        .restoreQueue({
          tracks,
          playOrder: [0, 1, 2],
          currentIndex: 99,
          source: null,
          shuffled: false,
        });

      expect(useQueueStore.getState().currentIndex).toBe(2);
    });

    it('clamps a negative currentIndex to 0', () => {
      const tracks = [track('a'), track('b'), track('c')];

      useQueueStore
        .getState()
        .restoreQueue({
          tracks,
          playOrder: [0, 1, 2],
          currentIndex: -4,
          source: null,
          shuffled: false,
        });

      expect(useQueueStore.getState().currentIndex).toBe(0);
    });

    it('forces currentIndex to -1 for an empty playOrder, regardless of the passed currentIndex', () => {
      const tracks = [track('a'), track('b'), track('c')];

      useQueueStore
        .getState()
        .restoreQueue({ tracks, playOrder: [], currentIndex: 2, source: null, shuffled: false });

      const state = useQueueStore.getState();
      expect(state.playOrder).toEqual([]);
      expect(state.currentIndex).toBe(-1);
    });

    it('keeps a playOrder entry pointing at a track deleted between save and restore, and resolves it to no current track', () => {
      const tracks = [track('a'), track('b'), track('c')];

      useQueueStore
        .getState()
        .restoreQueue({
          tracks,
          playOrder: [0, 3, 1, 2],
          currentIndex: 1,
          source: null,
          shuffled: false,
        });

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
        () =>
          useQueueStore
            .getState()
            .restoreQueue({
              tracks: [track('a')],
              playOrder: [0],
              currentIndex: 0,
              source: null,
              shuffled: false,
            }),
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

      useQueueStore
        .getState()
        .restoreQueue({
          tracks: [track('a'), track('b')],
          playOrder: [1, 0],
          currentIndex: 0,
          source: null,
          shuffled: true,
        });
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
            .restoreQueue({
              tracks: [track('a'), track('b'), track('c')],
              playOrder: [2, 0, 1],
              currentIndex: 1,
              source: null,
              shuffled: true,
            }),
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
            .restoreQueue({
              tracks: [track('a'), track('b'), track('c')],
              playOrder: [2, 0, 1],
              currentIndex: 1,
              source: null,
              shuffled: true,
            }),
        [2, 0, 1],
      ],
    ];

    it.each(seeds)(
      'flips the flag without touching playOrder, over %s',
      (_label, seed, expectedOrder) => {
        seed();
        const before = useQueueStore.getState().shuffled;

        useQueueStore.getState().setShuffled(!before);

        expect(useQueueStore.getState().shuffled).toBe(!before);
        expect(useQueueStore.getState().playOrder).toEqual(expectedOrder);
      },
    );

    it('setting the same value twice is idempotent', () => {
      useQueueStore
        .getState()
        .restoreQueue({
          tracks: [track('a'), track('b'), track('c')],
          playOrder: [2, 0, 1],
          currentIndex: 1,
          source: null,
          shuffled: false,
        });

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
            .restoreQueue({
              tracks: [track('a'), track('b'), track('c')],
              playOrder: [2, 0, 1],
              currentIndex: 1,
              source: PLAYLIST_SOURCE,
              shuffled: true,
            });
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
});

describe('navigation', () => {
  beforeEach(() => {
    useQueueStore.setState(INITIAL_STATE, true);
  });

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

    it('returns null when the play order slot points past the tracks it was built from', () => {
      seed([track('a'), track('b')], [0, 5, 1], 1);

      expect(useQueueStore.getState().currentTrack()).toBeNull();
    });

    it('returns null when the cursor sits past the end of the play order', () => {
      seed([track('a'), track('b')], [0, 1], 4);

      expect(useQueueStore.getState().currentTrack()).toBeNull();
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
    ])(
      'repeatMode=%s at index %i -> hasNext=%s hasPrevious=%s',
      (repeatMode, index, next, previous) => {
        seed([track('a'), track('b'), track('c')], [0, 1, 2], index, repeatMode);

        expect(useQueueStore.getState().hasNext()).toBe(next);
        expect(useQueueStore.getState().hasPrevious()).toBe(previous);
      },
    );

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

    it.each([
      ['NaN', Number.NaN],
      ['a fractional index', 0.5],
    ])('rejects %s, leaving the store state unchanged', (_label, index) => {
      seed([track('a'), track('b')], [0, 1], 1);
      const before = useQueueStore.getState();

      const result = useQueueStore.getState().skipToIndex(index);

      expect(result).toBeNull();
      expect(useQueueStore.getState()).toBe(before);
      expect(useQueueStore.getState().currentIndex).toBe(1);
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
});

describe('user-queued tracks', () => {
  function track(id: string): PlaybackTrack {
    return {
      source: { kind: 'library', trackId: asTrackId(id) },
      title: id,
      artist: 'Test Artist',
      artworkUrl: null,
    };
  }

  function loadList(ids: readonly string[], startIndex: number) {
    useQueueStore.getState().loadQueue(ids.map(track), startIndex, null);
  }

  function titles(): string[] {
    return orderedQueueTracks(useQueueStore.getState()).map((t) => t.title);
  }

  function upcomingTitles(): string[] {
    return titles().slice(useQueueStore.getState().currentIndex + 1);
  }

  const drawsArb = fc.array(fc.double({ min: 0, max: 0.999, noNaN: true }), {
    minLength: 1,
    maxLength: 20,
  });

  function withDraws(draws: readonly number[], run: () => void) {
    useQueueStore.setState(INITIAL_STATE, true);
    let i = 0;
    jest.spyOn(Math, 'random').mockImplementation(() => draws[i++ % draws.length]!);
    run();
    jest.restoreAllMocks();
  }

  beforeEach(() => {
    useQueueStore.setState(INITIAL_STATE, true);
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  describe('shuffle keeps a play-next Track immediately next', () => {
    it('repro: play a song, play-next another, shuffle -> the queued song is still next', () => {
      fc.assert(
        fc.property(drawsArb, (draws) => {
          withDraws(draws, () => {
            loadList(['a', 'b', 'c', 'd', 'e'], 0);
            useQueueStore.getState().playNext(track('queued'));

            useQueueStore.getState().toggleShuffle();

            const state = useQueueStore.getState();
            expect(state.shuffled).toBe(true);
            expect(state.currentIndex).toBe(0);
            expect(titles()[0]).toBe('a');
            expect(upcomingTitles()[0]).toBe('queued');
          });
        }),
      );
    });

    it('holds when the play-next is added mid-list, after skipping ahead', () => {
      fc.assert(
        fc.property(drawsArb, (draws) => {
          withDraws(draws, () => {
            loadList(['a', 'b', 'c', 'd', 'e', 'f'], 0);
            useQueueStore.getState().skipToIndex(2);
            useQueueStore.getState().playNext(track('queued'));

            useQueueStore.getState().toggleShuffle();

            expect(titles().slice(0, 3)).toEqual(['a', 'b', 'c']);
            expect(upcomingTitles()[0]).toBe('queued');
          });
        }),
      );
    });

    it('keeps several play-next Tracks ahead of the shuffled list, in their queued order', () => {
      fc.assert(
        fc.property(drawsArb, (draws) => {
          withDraws(draws, () => {
            loadList(['a', 'b', 'c', 'd', 'e'], 0);
            useQueueStore.getState().playNext(track('x'));
            useQueueStore.getState().playNext(track('y'));

            useQueueStore.getState().toggleShuffle();

            expect(upcomingTitles().slice(0, 2)).toEqual(['y', 'x']);
            expect([...upcomingTitles().slice(2)].sort()).toEqual(['b', 'c', 'd', 'e']);
          });
        }),
      );
    });

    it('stays next when shuffle is turned back off', () => {
      loadList(['a', 'b', 'c', 'd', 'e'], 0);
      useQueueStore.getState().playNext(track('queued'));
      jest.spyOn(Math, 'random').mockReturnValue(0);
      useQueueStore.getState().toggleShuffle();

      useQueueStore.getState().toggleShuffle();

      expect(useQueueStore.getState().shuffled).toBe(false);
      expect(titles()).toEqual(['a', 'queued', 'b', 'c', 'd', 'e']);
    });

    it('stays next after an earlier upcoming Track is removed (pins follow the reindex)', () => {
      fc.assert(
        fc.property(drawsArb, (draws) => {
          withDraws(draws, () => {
            loadList(['a', 'b', 'c', 'd', 'e'], 0);
            useQueueStore.getState().playNext(track('queued'));
            useQueueStore.getState().removeFromQueue(3);

            useQueueStore.getState().toggleShuffle();

            expect(upcomingTitles()[0]).toBe('queued');
            expect(titles()).toHaveLength(5);
          });
        }),
      );
    });
  });

  describe('add-to-end is distinct from play-next under shuffle', () => {
    it('an appended Track stays last instead of jumping ahead of the play-next Track', () => {
      fc.assert(
        fc.property(drawsArb, (draws) => {
          withDraws(draws, () => {
            loadList(['a', 'b', 'c', 'd', 'e'], 0);
            useQueueStore.getState().enqueue(track('appended'));
            useQueueStore.getState().playNext(track('next'));

            useQueueStore.getState().toggleShuffle();

            const upcoming = upcomingTitles();
            expect(upcoming[0]).toBe('next');
            expect(upcoming[upcoming.length - 1]).toBe('appended');
            expect([...upcoming.slice(1, -1)].sort()).toEqual(['b', 'c', 'd', 'e']);
          });
        }),
      );
    });

    it('an appended Track is never pulled forward to play next on shuffle', () => {
      loadList(['a', 'b', 'c', 'd'], 0);
      useQueueStore.getState().enqueue(track('appended'));
      jest.spyOn(Math, 'random').mockReturnValue(0);

      useQueueStore.getState().toggleShuffle();

      expect(titles()).toEqual(['a', 'c', 'd', 'b', 'appended']);
    });

    it('an appended Track is still last after shuffling back off', () => {
      loadList(['a', 'b', 'c', 'd'], 0);
      useQueueStore.getState().enqueue(track('appended'));
      jest.spyOn(Math, 'random').mockReturnValue(0);
      useQueueStore.getState().toggleShuffle();

      useQueueStore.getState().toggleShuffle();

      expect(titles()).toEqual(['a', 'b', 'c', 'd', 'appended']);
    });

    it('a bulk add keeps every Track it appended last, in the order they were added', () => {
      loadList(['a', 'b', 'c'], 0);
      useQueueStore.getState().playNext(track('next'));
      useQueueStore.getState().enqueueMany([track('bulk1'), track('bulk2'), track('bulk3')]);
      jest.spyOn(Math, 'random').mockReturnValue(0);

      useQueueStore.getState().toggleShuffle();

      expect(upcomingTitles().slice(-3)).toEqual(['bulk1', 'bulk2', 'bulk3']);
    });

    it('a bulk add returns the upcoming Tracks the queue holds once it has landed', () => {
      loadList(['a', 'b'], 0);

      const upcoming = useQueueStore.getState().enqueueMany([track('bulk1'), track('bulk2')]);

      expect(upcoming.map((t) => t.title)).toEqual(['b', 'bulk1', 'bulk2']);
    });

    it('loading a new list forgets earlier play-next and appended Tracks', () => {
      loadList(['a', 'b'], 0);
      useQueueStore.getState().playNext(track('next'));
      useQueueStore.getState().enqueue(track('appended'));

      loadList(['p', 'q', 'r'], 0);

      const state = useQueueStore.getState();
      expect(state.upNext).toEqual([]);
      expect(state.appended).toEqual([]);
    });
  });
});
