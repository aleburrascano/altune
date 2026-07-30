import fc from 'fast-check';

import { orderedQueueTracks, useQueueStore } from '../queueStore';
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

function identity(length: number): number[] {
  return Array.from({ length }, (_, i) => i);
}

type Action =
  | { type: 'enqueue' }
  | { type: 'playNext' }
  | { type: 'reorder'; from: number; to: number }
  | { type: 'remove'; index: number }
  | { type: 'toggleShuffle' }
  | { type: 'cycleRepeat' }
  | { type: 'skipNext' }
  | { type: 'skipToIndex'; index: number };

const indexArb = fc.integer({ min: -2, max: 12 });

const actionArb: fc.Arbitrary<Action> = fc.oneof(
  fc.constant<Action>({ type: 'enqueue' }),
  fc.constant<Action>({ type: 'playNext' }),
  fc.record({ type: fc.constant('reorder' as const), from: indexArb, to: indexArb }),
  fc.record({ type: fc.constant('remove' as const), index: indexArb }),
  fc.constant<Action>({ type: 'toggleShuffle' }),
  fc.constant<Action>({ type: 'cycleRepeat' }),
  fc.constant<Action>({ type: 'skipNext' }),
  fc.record({ type: fc.constant('skipToIndex' as const), index: indexArb }),
);

function runAction(action: Action, nextIdRef: { id: number }) {
  const s = useQueueStore.getState();
  switch (action.type) {
    case 'enqueue':
      s.enqueue(track(`n${nextIdRef.id++}`));
      return;
    case 'playNext':
      s.playNext(track(`n${nextIdRef.id++}`));
      return;
    case 'reorder':
      s.reorderQueue(action.from, action.to);
      return;
    case 'remove':
      s.removeFromQueue(action.index);
      return;
    case 'toggleShuffle':
      s.toggleShuffle();
      return;
    case 'cycleRepeat':
      s.cycleRepeatMode();
      return;
    case 'skipNext':
      s.skipToNext();
      return;
    case 'skipToIndex':
      s.skipToIndex(action.index);
      return;
  }
}

describe('law: playOrder is always an index permutation over tracks', () => {
  it('holds after every action in a random sequence, over a random starting Queue', () => {
    fc.assert(
      fc.property(
        fc.integer({ min: 0, max: 6 }),
        fc.array(actionArb, { maxLength: 40 }),
        (initialCount, actions) => {
          useQueueStore.setState(INITIAL_STATE, true);
          const seed = Array.from({ length: initialCount }, (_, i) => track(`seed${i}`));
          useQueueStore.getState().loadQueue(seed, initialCount > 0 ? 0 : -1, null);
          const nextIdRef = { id: 0 };

          for (const action of actions) {
            runAction(action, nextIdRef);

            const state = useQueueStore.getState();
            expect([...state.playOrder].sort((a, b) => a - b)).toEqual(
              identity(state.tracks.length),
            );
            expect(state.currentIndex).toBeGreaterThanOrEqual(-1);
            expect(state.currentIndex).toBeLessThanOrEqual(state.playOrder.length - 1);
            expect(orderedQueueTracks(state)).toHaveLength(state.playOrder.length);
          }
        },
      ),
      { numRuns: 300 },
    );
  });

  it.each<[string, number, Action[]]>([
    ['reorder alone on a two-track queue', 2, [{ type: 'reorder', from: 0, to: 1 }]],
    [
      'enqueue then reorder the newly appended entry to the front',
      1,
      [{ type: 'enqueue' }, { type: 'reorder', from: 1, to: 0 }],
    ],
    [
      'remove the current Track then reorder what remains',
      3,
      [
        { type: 'remove', index: 0 },
        { type: 'reorder', from: 0, to: 1 },
      ],
    ],
    [
      'shuffle, then remove the playing Track, then playNext',
      4,
      [{ type: 'toggleShuffle' }, { type: 'remove', index: 0 }, { type: 'playNext' }],
    ],
  ])('worked example: %s', (_label, initialCount, actions) => {
    useQueueStore.setState(INITIAL_STATE, true);
    const seed = Array.from({ length: initialCount }, (_, i) => track(`seed${i}`));
    useQueueStore.getState().loadQueue(seed, initialCount > 0 ? 0 : -1, null);
    const nextIdRef = { id: 0 };

    for (const action of actions) {
      runAction(action, nextIdRef);

      const state = useQueueStore.getState();
      expect([...state.playOrder].sort((a, b) => a - b)).toEqual(identity(state.tracks.length));
      expect(state.currentIndex).toBeGreaterThanOrEqual(-1);
      expect(state.currentIndex).toBeLessThanOrEqual(state.playOrder.length - 1);
      expect(orderedQueueTracks(state)).toHaveLength(state.playOrder.length);
    }
  });
});
