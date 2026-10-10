import { asTrackId } from '@shared/api-client/ids';
import type { PlaybackTrack } from '@shared/playback/playbackTrack';
import { useQueueStore } from '@shared/playback/queueStore';

import { MAX_SAVED_QUEUE_IDS, savedQueueLists } from '@features/playback/domain/queueSaveWindow';
import { libraryTrack, previewTrack } from '@features/playback/__tests__/fixtures';

const OVERFLOW = 2_000;
const BIG_QUEUE_SIZE = MAX_SAVED_QUEUE_IDS + OVERFLOW;
const LAST_INDEX = BIG_QUEUE_SIZE - 1;
const HALF = 2;
const MIDDLE_INDEX = BIG_QUEUE_SIZE / HALF;
const SHIFTED_START = BIG_QUEUE_SIZE - MAX_SAVED_QUEUE_IDS;
const DUPLICATE_COUNT = 3;
const AFTER_PREVIEW = HALF;

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
    load([a!, previewTrack(), c!], AFTER_PREVIEW);

    expect(savedQueueLists(useQueueStore.getState())).toEqual({
      track_ids: ['a', 'c'],
      natural_order: ['a', 'c'],
      current_index: 1,
    });
  });

  it.each([0, MIDDLE_INDEX, LAST_INDEX])(
    'caps a 12000-track queue at 10000 ids per list with the current track at %i',
    (index) => {
      const ids = Array.from({ length: BIG_QUEUE_SIZE }, (_, i) => `t${i}`);
      load(queueOf(...ids), index);

      const lists = savedQueueLists(useQueueStore.getState());

      expect(lists.track_ids.length).toBeLessThanOrEqual(MAX_SAVED_QUEUE_IDS);
      expect(lists.natural_order.length).toBeLessThanOrEqual(MAX_SAVED_QUEUE_IDS);
      expect(lists.track_ids[lists.current_index]).toBe(`t${index}`);
      expect(sorted(lists.natural_order)).toEqual(sorted(lists.track_ids));
    },
  );

  it('shifts the window when the same store is read again after the index moves', () => {
    const ids = Array.from({ length: BIG_QUEUE_SIZE }, (_, i) => `t${i}`);
    load(queueOf(...ids), 0);

    const first = savedQueueLists(useQueueStore.getState());
    useQueueStore.setState({ currentIndex: LAST_INDEX });
    const second = savedQueueLists(useQueueStore.getState());

    expect(first.track_ids[0]).toBe('t0');
    expect(second.track_ids[0]).toBe(`t${SHIFTED_START}`);
    expect(second.track_ids[second.current_index]).toBe(`t${LAST_INDEX}`);
  });

  it('keeps a duplicated id in natural_order as many times as it appears in the window', () => {
    const ids = ['d', 'x', 'd', 'y', 'd'];
    load(queueOf(...ids), 0);

    const lists = savedQueueLists(useQueueStore.getState());

    expect(lists.natural_order.filter((id) => id === 'd')).toHaveLength(DUPLICATE_COUNT);
    expect(lists.natural_order).toEqual(ids);
  });
});
