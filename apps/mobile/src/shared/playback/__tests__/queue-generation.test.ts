/**
 * `generation` is the queue's ownership token: it bumps whenever the queue is
 * REPLACED, so a slow async flow (resume) can prove the queue it started against
 * is still in play before it writes. Transitions WITHIN a queue must not bump it,
 * or resume would abort on an ordinary auto-advance.
 */

import { useQueueStore } from '../queueStore';
import type { PlaybackTrack } from '../types';

function track(id: string): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: id },
    title: id,
    artist: `${id}-artist`,
    artworkUrl: null,
  };
}

const gen = () => useQueueStore.getState().generation;

describe('queue generation', () => {
  beforeEach(() => {
    useQueueStore.getState().clearQueue();
  });

  it('bumps when a queue replaces another', () => {
    const before = gen();
    useQueueStore.getState().loadQueue([track('a'), track('b')], 0, null);
    expect(gen()).toBeGreaterThan(before);
  });

  it('bumps on restore', () => {
    const before = gen();
    useQueueStore.getState().restoreQueue([track('a'), track('b')], [1, 0], 0, null, true);
    expect(gen()).toBeGreaterThan(before);
  });

  it('does not bump on transitions within the same queue', () => {
    useQueueStore.getState().loadQueue([track('a'), track('b'), track('c')], 0, null);
    const owned = gen();

    useQueueStore.getState().syncCurrentIndex(1);
    useQueueStore.getState().skipToIndex(2);
    useQueueStore.getState().enqueue(track('d'));
    useQueueStore.getState().toggleShuffle();
    useQueueStore.getState().cycleRepeatMode();

    expect(gen()).toBe(owned);
  });

  // Resetting to INITIAL must not rewind the counter — a resume holding
  // generation 1 would otherwise see 1 again after a clear+load and wrongly
  // conclude it still owned the queue (ABA).
  it('never rewinds when the queue empties', () => {
    useQueueStore.getState().loadQueue([track('a')], 0, null);
    const owned = gen();

    useQueueStore.getState().clearQueue();
    expect(gen()).toBeGreaterThan(owned);

    const afterClear = gen();
    useQueueStore.getState().loadQueue([track('b')], 0, null);
    expect(gen()).toBeGreaterThan(afterClear);
  });

  it('never rewinds when the last track is removed', () => {
    useQueueStore.getState().loadQueue([track('a')], 0, null);
    const owned = gen();

    useQueueStore.getState().removeFromQueue(0);
    expect(useQueueStore.getState().tracks).toHaveLength(0);
    expect(gen()).toBeGreaterThan(owned);
  });
});
