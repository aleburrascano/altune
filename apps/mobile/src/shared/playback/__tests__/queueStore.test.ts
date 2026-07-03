import { useQueueStore } from '../queueStore';

import type { PlaybackTrack } from '../types';

function makeTrack(id: string): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: id },
    title: `Track ${id}`,
    artist: `Artist ${id}`,
    artworkUrl: null,
  };
}

const tracks = [makeTrack('a'), makeTrack('b'), makeTrack('c'), makeTrack('d'), makeTrack('e')];

function resetStore(): void {
  useQueueStore.getState().clearQueue();
}

beforeEach(resetStore);

describe('loadQueue', () => {
  it('sets tracks, identity play order, and current index', () => {
    useQueueStore.getState().loadQueue(tracks, 2, null);
    const s = useQueueStore.getState();
    expect(s.tracks).toEqual(tracks);
    expect(s.playOrder).toEqual([0, 1, 2, 3, 4]);
    expect(s.currentIndex).toBe(2);
    expect(s.shuffled).toBe(false);
  });

  it('stores queue source', () => {
    const source = { kind: 'playlist' as const, playlistId: 'p1', name: 'My Playlist' };
    useQueueStore.getState().loadQueue(tracks, 0, source);
    expect(useQueueStore.getState().source).toEqual(source);
  });
});

describe('currentTrack', () => {
  it('returns the track at playOrder[currentIndex]', () => {
    useQueueStore.getState().loadQueue(tracks, 1, null);
    expect(useQueueStore.getState().currentTrack()).toEqual(tracks[1]);
  });

  it('returns null for empty queue', () => {
    expect(useQueueStore.getState().currentTrack()).toBeNull();
  });
});

describe('skipToNext', () => {
  it('advances to next track', () => {
    useQueueStore.getState().loadQueue(tracks, 0, null);
    const next = useQueueStore.getState().skipToNext();
    expect(next).toEqual(tracks[1]);
    expect(useQueueStore.getState().currentIndex).toBe(1);
  });

  it('returns null at end with repeat off', () => {
    useQueueStore.getState().loadQueue(tracks, 4, null);
    const next = useQueueStore.getState().skipToNext();
    expect(next).toBeNull();
    expect(useQueueStore.getState().currentIndex).toBe(4);
  });

  it('wraps to first with repeat all', () => {
    useQueueStore.getState().loadQueue(tracks, 4, null);
    useQueueStore.getState().cycleRepeatMode(); // off -> all
    const next = useQueueStore.getState().skipToNext();
    expect(next).toEqual(tracks[0]);
    expect(useQueueStore.getState().currentIndex).toBe(0);
  });

  it('returns same track with repeat one', () => {
    useQueueStore.getState().loadQueue(tracks, 2, null);
    useQueueStore.getState().cycleRepeatMode(); // off -> all
    useQueueStore.getState().cycleRepeatMode(); // all -> one
    const next = useQueueStore.getState().skipToNext();
    expect(next).toEqual(tracks[2]);
    expect(useQueueStore.getState().currentIndex).toBe(2);
  });

  it('returns null for empty queue', () => {
    expect(useQueueStore.getState().skipToNext()).toBeNull();
  });
});

describe('skipToPrevious', () => {
  it('goes to previous track', () => {
    useQueueStore.getState().loadQueue(tracks, 3, null);
    const prev = useQueueStore.getState().skipToPrevious();
    expect(prev).toEqual(tracks[2]);
    expect(useQueueStore.getState().currentIndex).toBe(2);
  });

  it('stays at first track with repeat off', () => {
    useQueueStore.getState().loadQueue(tracks, 0, null);
    const prev = useQueueStore.getState().skipToPrevious();
    expect(prev).toEqual(tracks[0]);
    expect(useQueueStore.getState().currentIndex).toBe(0);
  });

  it('wraps to last with repeat all', () => {
    useQueueStore.getState().loadQueue(tracks, 0, null);
    useQueueStore.getState().cycleRepeatMode(); // off -> all
    const prev = useQueueStore.getState().skipToPrevious();
    expect(prev).toEqual(tracks[4]);
    expect(useQueueStore.getState().currentIndex).toBe(4);
  });
});

describe('skipToIndex', () => {
  it('jumps to the given index', () => {
    useQueueStore.getState().loadQueue(tracks, 0, null);
    const track = useQueueStore.getState().skipToIndex(3);
    expect(track).toEqual(tracks[3]);
    expect(useQueueStore.getState().currentIndex).toBe(3);
  });

  it('returns null for out-of-bounds', () => {
    useQueueStore.getState().loadQueue(tracks, 0, null);
    expect(useQueueStore.getState().skipToIndex(-1)).toBeNull();
    expect(useQueueStore.getState().skipToIndex(10)).toBeNull();
  });
});

describe('toggleShuffle', () => {
  it('shuffles only the upcoming tracks, keeping current and history in place', () => {
    useQueueStore.getState().loadQueue(tracks, 2, null);
    useQueueStore.getState().toggleShuffle();
    const s = useQueueStore.getState();
    expect(s.shuffled).toBe(true);
    // current track and everything before it are untouched...
    expect(s.currentIndex).toBe(2);
    expect(s.playOrder.slice(0, 3)).toEqual([0, 1, 2]);
    expect(s.currentTrack()).toEqual(tracks[2]);
    // ...only the upcoming tail is reordered (still the same set of tracks).
    expect([...s.playOrder.slice(3)].sort()).toEqual([3, 4]);
    expect(s.playOrder.length).toBe(5);
  });

  it('unshuffles and finds current track in identity order', () => {
    useQueueStore.getState().loadQueue(tracks, 2, null);
    useQueueStore.getState().toggleShuffle();
    useQueueStore.getState().toggleShuffle();
    const s = useQueueStore.getState();
    expect(s.shuffled).toBe(false);
    expect(s.playOrder).toEqual([0, 1, 2, 3, 4]);
    expect(s.currentTrack()).toEqual(tracks[2]);
    expect(s.currentIndex).toBe(2);
  });

  it('does nothing for single-track queue', () => {
    useQueueStore.getState().loadQueue([tracks[0]!], 0, null);
    useQueueStore.getState().toggleShuffle();
    expect(useQueueStore.getState().shuffled).toBe(false);
  });
});

describe('cycleRepeatMode', () => {
  it('cycles off -> all -> one -> off', () => {
    useQueueStore.getState().loadQueue(tracks, 0, null);
    expect(useQueueStore.getState().repeatMode).toBe('off');
    useQueueStore.getState().cycleRepeatMode();
    expect(useQueueStore.getState().repeatMode).toBe('all');
    useQueueStore.getState().cycleRepeatMode();
    expect(useQueueStore.getState().repeatMode).toBe('one');
    useQueueStore.getState().cycleRepeatMode();
    expect(useQueueStore.getState().repeatMode).toBe('off');
  });
});

describe('hasNext / hasPrevious', () => {
  it('hasNext is false at end with repeat off', () => {
    useQueueStore.getState().loadQueue(tracks, 4, null);
    expect(useQueueStore.getState().hasNext()).toBe(false);
  });

  it('hasNext is true at end with repeat all', () => {
    useQueueStore.getState().loadQueue(tracks, 4, null);
    useQueueStore.getState().cycleRepeatMode();
    expect(useQueueStore.getState().hasNext()).toBe(true);
  });

  it('hasPrevious is false at start with repeat off', () => {
    useQueueStore.getState().loadQueue(tracks, 0, null);
    expect(useQueueStore.getState().hasPrevious()).toBe(false);
  });

  it('hasPrevious is true at start with repeat all', () => {
    useQueueStore.getState().loadQueue(tracks, 0, null);
    useQueueStore.getState().cycleRepeatMode();
    expect(useQueueStore.getState().hasPrevious()).toBe(true);
  });
});

describe('clearQueue', () => {
  it('resets to empty state', () => {
    useQueueStore.getState().loadQueue(tracks, 2, { kind: 'library' });
    useQueueStore.getState().clearQueue();
    const s = useQueueStore.getState();
    expect(s.tracks).toEqual([]);
    expect(s.playOrder).toEqual([]);
    expect(s.currentIndex).toBe(-1);
    expect(s.source).toBeNull();
  });
});

describe('enqueue', () => {
  it('appends a track to the end of the play order', () => {
    useQueueStore.getState().loadQueue(tracks, 0, null);
    const extra = makeTrack('z');
    useQueueStore.getState().enqueue(extra);
    const s = useQueueStore.getState();
    expect(s.tracks).toHaveLength(6);
    expect(s.playOrder).toEqual([0, 1, 2, 3, 4, 5]);
    expect(s.tracks[5]).toEqual(extra);
    expect(s.currentIndex).toBe(0);
  });

  it('appends at the tail even when shuffled, without disturbing current/history', () => {
    useQueueStore.getState().loadQueue(tracks, 2, null);
    useQueueStore.getState().toggleShuffle();
    const orderBefore = [...useQueueStore.getState().playOrder];
    const extra = makeTrack('z');
    useQueueStore.getState().enqueue(extra);
    const s = useQueueStore.getState();
    // the new track's index (5) lands last in the play sequence...
    expect(s.playOrder[s.playOrder.length - 1]).toBe(5);
    // ...and the existing order (current + history + upcoming) is untouched.
    expect(s.playOrder.slice(0, orderBefore.length)).toEqual(orderBefore);
    expect(s.currentIndex).toBe(2);
    expect(s.currentTrack()).toEqual(tracks[2]);
  });

  it('starts a one-track order from an empty queue', () => {
    const extra = makeTrack('z');
    useQueueStore.getState().enqueue(extra);
    const s = useQueueStore.getState();
    expect(s.tracks).toEqual([extra]);
    expect(s.playOrder).toEqual([0]);
  });
});

describe('playNext', () => {
  it('inserts a track right after the current one', () => {
    useQueueStore.getState().loadQueue(tracks, 1, null);
    const extra = makeTrack('z');
    useQueueStore.getState().playNext(extra);
    const s = useQueueStore.getState();
    expect(s.tracks).toHaveLength(6);
    // new track index is 5; it sits at play-order position currentIndex+1 = 2.
    expect(s.playOrder).toEqual([0, 1, 5, 2, 3, 4]);
    // current track and index are unchanged.
    expect(s.currentIndex).toBe(1);
    expect(s.currentTrack()).toEqual(tracks[1]);
    // the very next track is now the inserted one.
    const next = useQueueStore.getState().skipToNext();
    expect(next).toEqual(extra);
  });

  it('inserts after current even when shuffled', () => {
    useQueueStore.getState().loadQueue(tracks, 2, null);
    useQueueStore.getState().toggleShuffle();
    const insertAt = useQueueStore.getState().currentIndex + 1;
    const extra = makeTrack('z');
    useQueueStore.getState().playNext(extra);
    const s = useQueueStore.getState();
    expect(s.playOrder[insertAt]).toBe(5);
    expect(s.currentIndex).toBe(2);
    expect(s.currentTrack()).toEqual(tracks[2]);
  });
});

describe('removeFromQueue', () => {
  it('removes a track and adjusts indices', () => {
    useQueueStore.getState().loadQueue(tracks, 0, null);
    useQueueStore.getState().removeFromQueue(2);
    const s = useQueueStore.getState();
    expect(s.tracks.length).toBe(4);
    expect(s.playOrder.length).toBe(4);
    expect(s.currentIndex).toBe(0);
  });

  it('clears queue when last track removed', () => {
    useQueueStore.getState().loadQueue([tracks[0]!], 0, null);
    useQueueStore.getState().removeFromQueue(0);
    expect(useQueueStore.getState().tracks.length).toBe(0);
  });
});
