import { asTrackId } from '@shared/api-client/ids';
import type { PlaybackTrack } from '@shared/playback/playbackTrack';
import { useQueueStore } from '@shared/playback/queueStore';

import { savedQueueLists } from '@features/playback/domain/queueSaveWindow';
import { libraryTrack, previewTrack } from '@features/playback/__tests__/fixtures';

function queueOf(...ids: string[]): PlaybackTrack[] {
  return ids.map((id) => libraryTrack({ source: { kind: 'library', trackId: asTrackId(id) } }));
}

function load(queue: PlaybackTrack[], index: number): void {
  useQueueStore.getState().clearQueue();
  useQueueStore.getState().loadQueue(queue, index, null);
}

function sorted(ids: string[]): string[] {
  return [...ids].sort();
}

describe('savedQueueLists', () => {
  beforeEach(() => {
    useQueueStore.getState().clearQueue();
  });

  it('returns empty lists for an empty queue', () => {
    expect(savedQueueLists(useQueueStore.getState())).toEqual({
      track_ids: [],
      natural_order: [],
      current_index: 0,
    });
  });

  it('returns the one id of a one-track library queue', () => {
    load(queueOf('a'), 0);

    expect(savedQueueLists(useQueueStore.getState())).toEqual({
      track_ids: ['a'],
      natural_order: ['a'],
      current_index: 0,
    });
  });

  it('leaves out a preview track between library tracks and counts library tracks only', () => {
    const [a, c] = queueOf('a', 'c');
    load([a!, previewTrack(), c!], 2);

    expect(savedQueueLists(useQueueStore.getState())).toEqual({
      track_ids: ['a', 'c'],
      natural_order: ['a', 'c'],
      current_index: 1,
    });
  });

  it.each([0, 6_000, 11_999])(
    'caps a 12000-track queue at 10000 ids per list with the current track at %i',
    (index) => {
      const ids = Array.from({ length: 12_000 }, (_, i) => `t${i}`);
      load(queueOf(...ids), index);

      const lists = savedQueueLists(useQueueStore.getState());

      expect(lists.track_ids.length).toBeLessThanOrEqual(10_000);
      expect(lists.natural_order.length).toBeLessThanOrEqual(10_000);
      expect(lists.track_ids[lists.current_index]).toBe(`t${index}`);
      expect(sorted(lists.natural_order)).toEqual(sorted(lists.track_ids));
    },
  );

  it('shifts the window when the same store is read again after the index moves', () => {
    const ids = Array.from({ length: 12_000 }, (_, i) => `t${i}`);
    load(queueOf(...ids), 0);

    const first = savedQueueLists(useQueueStore.getState());
    useQueueStore.setState({ currentIndex: 11_999 });
    const second = savedQueueLists(useQueueStore.getState());

    expect(first.track_ids[0]).toBe('t0');
    expect(second.track_ids[0]).toBe('t2000');
    expect(second.track_ids[second.current_index]).toBe('t11999');
  });

  it('keeps a duplicated id in natural_order as many times as it appears in the window', () => {
    const ids = ['d', 'x', 'd', 'y', 'd'];
    load(queueOf(...ids), 0);

    const lists = savedQueueLists(useQueueStore.getState());

    expect(lists.natural_order.filter((id) => id === 'd')).toHaveLength(3);
    expect(lists.natural_order).toEqual(ids);
  });
});
