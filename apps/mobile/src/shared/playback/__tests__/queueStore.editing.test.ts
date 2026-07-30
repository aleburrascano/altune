import fc from 'fast-check';

import { orderedQueueTracks, useQueueStore } from '../queueStore';
import { trackKey } from '../trackKey';
import type { PlaybackTrack } from '../types';

const INITIAL_STATE = useQueueStore.getState();

function track(id: string): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: id },
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
  it.each<['off' | 'all' | 'one']>([['off'], ['all'], ['one']])('sets repeatMode to %s directly', (mode) => {
    useQueueStore.getState().setRepeatMode(mode);

    expect(useQueueStore.getState().repeatMode).toBe(mode);
  });
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
