import fc from 'fast-check';

import { usePinnedStore } from '../pinnedStore';

jest.mock('@shared/api-client/audio', () => ({
  fetchAudioUrls: jest.fn().mockResolvedValue([]),
}));

const INITIAL_STATE = usePinnedStore.getState();

async function flushAsync(): Promise<void> {
  for (let i = 0; i < 20; i += 1) {
    await Promise.resolve();
  }
}

const ID_POOL = ['a', 'b', 'c'] as const;
const idArb = fc.constantFrom(...ID_POOL);

type Action =
  | { type: 'pin'; id: string }
  | { type: 'pinMany'; ids: string[] }
  | { type: 'unpin'; id: string }
  | { type: 'unpinAll' };

const actionArb: fc.Arbitrary<Action> = fc.oneof(
  fc.record({ type: fc.constant('pin' as const), id: idArb }),
  fc.record({ type: fc.constant('pinMany' as const), ids: fc.array(idArb, { maxLength: 3 }) }),
  fc.record({ type: fc.constant('unpin' as const), id: idArb }),
  fc.constant<Action>({ type: 'unpinAll' }),
);

function runAction(action: Action): void {
  const s = usePinnedStore.getState();
  switch (action.type) {
    case 'pin':
      s.pin(action.id);
      return;
    case 'pinMany':
      s.pinMany(action.ids);
      return;
    case 'unpin':
      s.unpin(action.id);
      return;
    case 'unpinAll':
      s.unpinAll();
      return;
  }
}

function assertQueueEntriesConsistent(): void {
  const { entries, queue } = usePinnedStore.getState();
  for (const id of queue) {
    expect(entries[id]).toBeDefined();
  }
}

describe('law: every id present in queue also has an entry in entries', () => {
  it('holds after every action in a random sequence, checked after each step', async () => {
    await fc.assert(
      fc.asyncProperty(fc.array(actionArb, { maxLength: 25 }), async (actions) => {
        usePinnedStore.setState(INITIAL_STATE, true);

        for (const action of actions) {
          runAction(action);
          assertQueueEntriesConsistent();
        }

        await flushAsync();
      }),
      { numRuns: 200 },
    );
  });

  it.each<[string, Action[]]>([
    ['pin then unpin the same id', [{ type: 'pin', id: 'a' }, { type: 'unpin', id: 'a' }]],
    [
      'pinMany over several ids, only the first synchronously advances',
      [{ type: 'pinMany', ids: ['a', 'b', 'c'] }],
    ],
    [
      'pin one id while another is already mid-download, then unpinAll',
      [{ type: 'pin', id: 'a' }, { type: 'pin', id: 'b' }, { type: 'unpinAll' }],
    ],
    [
      'unpin an id that was never pinned',
      [{ type: 'unpin', id: 'ghost' }],
    ],
    [
      'pinMany twice over overlapping ids while the worker is busy',
      [
        { type: 'pinMany', ids: ['a', 'b'] },
        { type: 'pinMany', ids: ['a', 'b'] },
      ],
    ],
  ])('worked example: %s', async (_label, actions) => {
    usePinnedStore.setState(INITIAL_STATE, true);

    for (const action of actions) {
      runAction(action);
      assertQueueEntriesConsistent();
    }

    await flushAsync();
  });
});
