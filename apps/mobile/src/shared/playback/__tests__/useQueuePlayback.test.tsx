import type { ReactNode } from 'react';

import { act, renderHook } from '@testing-library/react-native';

import { useQueuePlayback } from '../useQueuePlayback';
import { useQueueStore } from '../queueStore';
import { PlaybackContext } from '../PlaybackContext';
import type { PlaybackContextValue, PlaybackTrack } from '../types';

const INITIAL_STATE = useQueueStore.getState();

beforeEach(() => {
  useQueueStore.setState(INITIAL_STATE, true);
});

function track(id: string): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: id },
    title: `Title ${id}`,
    artist: `Artist ${id}`,
    artworkUrl: null,
  };
}

function resolvedRejection(): Promise<never> {
  const rejected = Promise.reject(new Error('native call failed'));
  rejected.catch(() => undefined);
  return rejected;
}

function makeControls(overrides: Partial<PlaybackContextValue> = {}): PlaybackContextValue {
  return {
    status: 'idle',
    track: null,
    positionMs: 0,
    durationMs: 0,
    errorMessage: null,
    play: jest.fn().mockResolvedValue(undefined),
    startQueue: jest.fn().mockResolvedValue(undefined),
    skipToQueueIndex: jest.fn().mockResolvedValue(undefined),
    reorderUpcoming: jest.fn().mockResolvedValue(undefined),
    appendToQueue: jest.fn().mockResolvedValue(undefined),
    insertNext: jest.fn().mockResolvedValue(undefined),
    skipNext: jest.fn().mockResolvedValue(undefined),
    skipPrevious: jest.fn().mockResolvedValue(undefined),
    removeQueueIndex: jest.fn().mockResolvedValue(undefined),
    pause: jest.fn(),
    resume: jest.fn(),
    seekTo: jest.fn(),
    setRate: jest.fn(),
    stop: jest.fn(),
    retry: jest.fn(),
    ...overrides,
  };
}

function wrapperFor(controls: PlaybackContextValue) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <PlaybackContext.Provider value={controls}>{children}</PlaybackContext.Provider>;
  };
}

function setup(controls: PlaybackContextValue) {
  return renderHook(() => useQueuePlayback(), { wrapper: wrapperFor(controls) });
}

describe('playFromList / playTrack', () => {
  it('hands native the post-load index, not the caller index, when every Track was filtered out', () => {
    const controls = makeControls({});
    const { result } = setup(controls);

    act(() => {
      result.current.playFromList([], 3, { kind: 'library' });
    });

    expect(controls.startQueue).toHaveBeenCalledWith([], -1);
    expect(useQueueStore.getState().currentIndex).toBe(-1);
  });

  it('loads the store before dispatching startQueue, handing native the post-load order and index', () => {
    act(() => {
      useQueueStore.getState().loadQueue([track('old')], 0, null);
    });
    const capturedAtCall: { tracks?: readonly PlaybackTrack[]; currentIndex?: number } = {};
    const controls = makeControls({
      startQueue: jest.fn(() => {
        capturedAtCall.tracks = useQueueStore.getState().tracks;
        capturedAtCall.currentIndex = useQueueStore.getState().currentIndex;
        return Promise.resolve();
      }),
    });
    const { result } = setup(controls);
    const newTracks = [track('a'), track('b'), track('c')];

    act(() => {
      result.current.playFromList(newTracks, 2, { kind: 'library' });
    });

    expect(controls.startQueue).toHaveBeenCalledWith(newTracks, 2);
    expect(capturedAtCall.currentIndex).toBe(2);
    expect(capturedAtCall.tracks).toEqual(newTracks);
  });

  it('playTrack loads a singleton queue and starts native playback at index 0', () => {
    const controls = makeControls();
    const { result } = setup(controls);
    const solo = track('solo');

    act(() => {
      result.current.playTrack(solo);
    });

    const state = useQueueStore.getState();
    expect(state.tracks).toEqual([solo]);
    expect(state.playOrder).toEqual([0]);
    expect(state.currentIndex).toBe(0);
    expect(controls.startQueue).toHaveBeenCalledWith([solo], 0);
  });

  it('leaves the store loaded even when the native startQueue call rejects', () => {
    const controls = makeControls({ startQueue: jest.fn(() => resolvedRejection()) });
    const { result } = setup(controls);
    const newTracks = [track('a'), track('b')];

    act(() => {
      result.current.playFromList(newTracks, 1, null);
    });

    const state = useQueueStore.getState();
    expect(state.tracks).toEqual(newTracks);
    expect(state.currentIndex).toBe(1);
  });
});

describe('addToQueue', () => {
  it('plays the track immediately instead of enqueueing when the queue is empty', () => {
    const controls = makeControls();
    const { result } = setup(controls);
    const t = track('a');

    act(() => {
      result.current.addToQueue(t);
    });

    expect(useQueueStore.getState().tracks).toEqual([t]);
    expect(controls.startQueue).toHaveBeenCalledWith([t], 0);
    expect(controls.appendToQueue).not.toHaveBeenCalled();
  });

  it('enqueues the track and appends it natively when the queue already has entries', () => {
    act(() => {
      useQueueStore.getState().loadQueue([track('a')], 0, null);
    });
    const controls = makeControls();
    const { result } = setup(controls);
    const t = track('b');

    act(() => {
      result.current.addToQueue(t);
    });

    const state = useQueueStore.getState();
    expect(state.tracks).toEqual([track('a'), t]);
    expect(state.playOrder).toEqual([0, 1]);
    expect(controls.appendToQueue).toHaveBeenCalledWith(t);
    expect(controls.startQueue).not.toHaveBeenCalled();
  });

  it('leaves the enqueue committed even when the native append call rejects', () => {
    act(() => {
      useQueueStore.getState().loadQueue([track('a')], 0, null);
    });
    const controls = makeControls({ appendToQueue: jest.fn(() => resolvedRejection()) });
    const { result } = setup(controls);
    const t = track('b');

    act(() => {
      result.current.addToQueue(t);
    });

    expect(useQueueStore.getState().tracks).toEqual([track('a'), t]);
  });
});

describe('playNext', () => {
  it('plays the track immediately when the queue is empty', () => {
    const controls = makeControls();
    const { result } = setup(controls);
    const t = track('a');

    act(() => {
      result.current.playNext(t);
    });

    expect(useQueueStore.getState().tracks).toEqual([t]);
    expect(controls.startQueue).toHaveBeenCalledWith([t], 0);
    expect(controls.insertNext).not.toHaveBeenCalled();
    expect(controls.appendToQueue).not.toHaveBeenCalled();
  });

  it('appends rather than inserts when the current track is last in the queue', () => {
    act(() => {
      useQueueStore.getState().loadQueue([track('a'), track('b')], 1, null);
    });
    const controls = makeControls();
    const { result } = setup(controls);
    const t = track('c');

    act(() => {
      result.current.playNext(t);
    });

    expect(controls.appendToQueue).toHaveBeenCalledWith(t);
    expect(controls.insertNext).not.toHaveBeenCalled();
    const state = useQueueStore.getState();
    expect(state.tracks).toEqual([track('a'), track('b'), t]);
    expect(state.playOrder).toEqual([0, 1, 2]);
  });

  it('inserts at the position after the current track, already in range of the post-mutation queue', () => {
    act(() => {
      useQueueStore.getState().loadQueue([track('a'), track('b'), track('c')], 0, null);
    });
    let playOrderLengthAtCall = -1;
    const controls = makeControls({
      insertNext: jest.fn(() => {
        playOrderLengthAtCall = useQueueStore.getState().playOrder.length;
        return Promise.resolve();
      }),
    });
    const { result } = setup(controls);
    const t = track('d');

    act(() => {
      result.current.playNext(t);
    });

    expect(controls.insertNext).toHaveBeenCalledWith(t, 1);
    expect(playOrderLengthAtCall).toBe(4);
    const state = useQueueStore.getState();
    expect(state.playOrder.map((i) => state.tracks[i])).toEqual([
      track('a'),
      t,
      track('b'),
      track('c'),
    ]);
  });

  it('leaves the insert committed even when the native insertNext call rejects', () => {
    act(() => {
      useQueueStore.getState().loadQueue([track('a'), track('b'), track('c')], 0, null);
    });
    const controls = makeControls({ insertNext: jest.fn(() => resolvedRejection()) });
    const { result } = setup(controls);
    const t = track('d');

    act(() => {
      result.current.playNext(t);
    });

    const state = useQueueStore.getState();
    expect(state.playOrder.map((i) => state.tracks[i])).toEqual([
      track('a'),
      t,
      track('b'),
      track('c'),
    ]);
  });
});

describe('skipToIndex / removeFromQueue / moveQueueItem', () => {
  it('skipToIndex moves the store to the tapped row and mirrors it to native', () => {
    act(() => {
      useQueueStore.getState().loadQueue([track('a'), track('b'), track('c')], 0, null);
    });
    const controls = makeControls();
    const { result } = setup(controls);

    act(() => {
      result.current.skipToIndex(2);
    });

    expect(useQueueStore.getState().currentIndex).toBe(2);
    expect(useQueueStore.getState().currentTrack()).toEqual(track('c'));
    expect(controls.skipToQueueIndex).toHaveBeenCalledWith(2);
  });

  it('leaves the store on the tapped row even when native skipToQueueIndex rejects', () => {
    act(() => {
      useQueueStore.getState().loadQueue([track('a'), track('b'), track('c')], 0, null);
    });
    const controls = makeControls({ skipToQueueIndex: jest.fn(() => resolvedRejection()) });
    const { result } = setup(controls);

    act(() => {
      result.current.skipToIndex(2);
    });

    expect(useQueueStore.getState().currentIndex).toBe(2);
  });

  it('removeFromQueue drops the swiped row from both the store and native', () => {
    act(() => {
      useQueueStore.getState().loadQueue([track('a'), track('b'), track('c')], 0, null);
    });
    const controls = makeControls();
    const { result } = setup(controls);

    act(() => {
      result.current.removeFromQueue(2);
    });

    const state = useQueueStore.getState();
    expect(state.tracks).toEqual([track('a'), track('b')]);
    expect(controls.removeQueueIndex).toHaveBeenCalledWith(2);
  });

  it('leaves the row removed from the store even when native removeQueueIndex rejects', () => {
    act(() => {
      useQueueStore.getState().loadQueue([track('a'), track('b'), track('c')], 0, null);
    });
    const controls = makeControls({ removeQueueIndex: jest.fn(() => resolvedRejection()) });
    const { result } = setup(controls);

    act(() => {
      result.current.removeFromQueue(2);
    });

    expect(useQueueStore.getState().tracks).toEqual([track('a'), track('b')]);
  });

  it('moveQueueItem recomputes Up Next from the store after the reorder, not before', () => {
    act(() => {
      useQueueStore
        .getState()
        .loadQueue([track('a'), track('b'), track('c'), track('d')], 1, null);
    });
    let playOrderAtCall: readonly number[] = [];
    const controls = makeControls({
      reorderUpcoming: jest.fn(() => {
        playOrderAtCall = useQueueStore.getState().playOrder;
        return Promise.resolve();
      }),
    });
    const { result } = setup(controls);

    act(() => {
      result.current.moveQueueItem(2, 3);
    });

    expect(playOrderAtCall).toEqual([0, 1, 3, 2]);
    expect(controls.reorderUpcoming).toHaveBeenCalledWith([track('d'), track('c')]);
  });

  it('leaves the reorder committed in the store even when native reorderUpcoming rejects', () => {
    act(() => {
      useQueueStore
        .getState()
        .loadQueue([track('a'), track('b'), track('c'), track('d')], 1, null);
    });
    const controls = makeControls({ reorderUpcoming: jest.fn(() => resolvedRejection()) });
    const { result } = setup(controls);

    act(() => {
      result.current.moveQueueItem(2, 3);
    });

    expect(useQueueStore.getState().playOrder).toEqual([0, 1, 3, 2]);
  });
});

describe('clearUpcoming', () => {
  it('removes upcoming rows from native in descending order and never the playing track', () => {
    act(() => {
      useQueueStore
        .getState()
        .loadQueue([track('a'), track('b'), track('c'), track('d'), track('e')], 1, null);
    });
    const removeQueueIndex = jest.fn().mockResolvedValue(undefined);
    const controls = makeControls({ removeQueueIndex });
    const { result } = setup(controls);

    act(() => {
      result.current.clearUpcoming();
    });

    expect(removeQueueIndex.mock.calls).toEqual([[4], [3], [2]]);
    const state = useQueueStore.getState();
    expect(state.tracks).toEqual([track('a'), track('b')]);
    expect(state.currentTrack()).toEqual(track('b'));
  });

  it('still clears every upcoming row from the store when every native removal rejects', () => {
    act(() => {
      useQueueStore
        .getState()
        .loadQueue([track('a'), track('b'), track('c'), track('d'), track('e')], 1, null);
    });
    const controls = makeControls({ removeQueueIndex: jest.fn(() => resolvedRejection()) });
    const { result } = setup(controls);

    act(() => {
      result.current.clearUpcoming();
    });

    expect(controls.removeQueueIndex).toHaveBeenCalledTimes(3);
    const state = useQueueStore.getState();
    expect(state.tracks).toEqual([track('a'), track('b')]);
    expect(state.currentTrack()).toEqual(track('b'));
  });
});

describe('toggleShuffle / cycleRepeatMode', () => {
  it('hands native only the shuffled upcoming slice, never restarting the playing track', () => {
    const randomSpy = jest.spyOn(Math, 'random').mockReturnValue(0);
    act(() => {
      useQueueStore
        .getState()
        .loadQueue([track('a'), track('b'), track('c'), track('d')], 1, null);
    });
    const controls = makeControls();
    const { result } = setup(controls);

    act(() => {
      result.current.toggleShuffle();
    });

    expect(controls.reorderUpcoming).toHaveBeenCalledWith([track('d'), track('c')]);
    const state = useQueueStore.getState();
    expect(state.shuffled).toBe(true);
    expect(state.currentTrack()).toEqual(track('b'));
    randomSpy.mockRestore();
  });

  it('leaves the shuffle committed in the store even when native reorderUpcoming rejects', () => {
    const randomSpy = jest.spyOn(Math, 'random').mockReturnValue(0);
    act(() => {
      useQueueStore
        .getState()
        .loadQueue([track('a'), track('b'), track('c'), track('d')], 1, null);
    });
    const controls = makeControls({ reorderUpcoming: jest.fn(() => resolvedRejection()) });
    const { result } = setup(controls);

    act(() => {
      result.current.toggleShuffle();
    });

    expect(useQueueStore.getState().shuffled).toBe(true);
    randomSpy.mockRestore();
  });

  it('cycleRepeatMode advances off -> all -> one -> off without any native call', () => {
    const controls = makeControls();
    const { result } = setup(controls);

    act(() => {
      result.current.cycleRepeatMode();
    });
    expect(useQueueStore.getState().repeatMode).toBe('all');

    act(() => {
      result.current.cycleRepeatMode();
    });
    expect(useQueueStore.getState().repeatMode).toBe('one');

    act(() => {
      result.current.cycleRepeatMode();
    });
    expect(useQueueStore.getState().repeatMode).toBe('off');
  });
});

describe('skipToNext / skipToPrevious', () => {
  it('skipToNext calls native skipNext without mutating the store', () => {
    act(() => {
      useQueueStore.getState().loadQueue([track('a'), track('b'), track('c')], 1, null);
    });
    const controls = makeControls();
    const { result } = setup(controls);
    const beforeState = useQueueStore.getState();

    act(() => {
      result.current.skipToNext();
    });

    expect(useQueueStore.getState()).toBe(beforeState);
    expect(controls.skipNext).toHaveBeenCalledTimes(1);
  });

  it('skipToPrevious calls native skipPrevious without mutating the store', () => {
    act(() => {
      useQueueStore.getState().loadQueue([track('a'), track('b'), track('c')], 1, null);
    });
    const controls = makeControls();
    const { result } = setup(controls);
    const beforeState = useQueueStore.getState();

    act(() => {
      result.current.skipToPrevious();
    });

    expect(useQueueStore.getState()).toBe(beforeState);
    expect(controls.skipPrevious).toHaveBeenCalledTimes(1);
  });

  it('skipToNext does not throw when native skipNext rejects', () => {
    act(() => {
      useQueueStore.getState().loadQueue([track('a'), track('b'), track('c')], 1, null);
    });
    const controls = makeControls({ skipNext: jest.fn(() => resolvedRejection()) });
    const { result } = setup(controls);
    const beforeState = useQueueStore.getState();

    expect(() => {
      act(() => {
        result.current.skipToNext();
      });
    }).not.toThrow();

    expect(useQueueStore.getState()).toBe(beforeState);
  });
});
