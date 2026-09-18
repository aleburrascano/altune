import fc from 'fast-check';

import { asTrackId } from '@shared/api-client/ids';

import { orderedQueueTracks, useQueueStore } from '../queueStore';
import type { PlaybackTrack } from '../types';

const INITIAL_STATE = useQueueStore.getState();

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
