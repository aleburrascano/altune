import { nextLifecycle, type LifecycleEvent, type LifecycleState } from '@shared/acquisition';
import { canRetry, isFailed, isPending, isReady } from '@shared/catalog';

const NOTHING: LifecycleState = { status: undefined, phase: undefined };

describe('nextLifecycle', () => {
  it.each<[string, LifecycleState, LifecycleEvent, LifecycleState | null]>([
    ['a start from nothing', NOTHING, { type: 'started' }, { status: 'pending', phase: 'finding' }],
    [
      'a repeated start while still finding',
      { status: 'pending', phase: 'finding' },
      { type: 'started' },
      { status: 'pending', phase: 'finding' },
    ],
    ['a start on a ready track', { status: 'ready', phase: undefined }, { type: 'started' }, null],
    [
      'a start once downloading',
      { status: 'pending', phase: 'downloading' },
      { type: 'started' },
      null,
    ],
    ['a start after done', { status: 'pending', phase: 'done' }, { type: 'started' }, null],
    ['a start after failed', { status: 'failed', phase: 'failed' }, { type: 'started' }, null],
    [
      'progress forward',
      { status: 'pending', phase: 'finding' },
      { type: 'progress', phase: 'downloading' },
      { status: 'pending', phase: 'downloading' },
    ],
    [
      'progress to the same phase',
      { status: 'pending', phase: 'downloading' },
      { type: 'progress', phase: 'downloading' },
      { status: 'pending', phase: 'downloading' },
    ],
    [
      'progress backwards',
      { status: 'pending', phase: 'finishing' },
      { type: 'progress', phase: 'finding' },
      null,
    ],
    [
      'progress after done',
      { status: 'ready', phase: 'done' },
      { type: 'progress', phase: 'downloading' },
      null,
    ],
    [
      'progress for a track with no phase yet',
      NOTHING,
      { type: 'progress', phase: 'downloading' },
      { status: undefined, phase: 'downloading' },
    ],
    [
      'completion',
      { status: 'pending', phase: 'downloading' },
      { type: 'completed' },
      { status: 'ready', phase: 'finishing' },
    ],
    [
      'failure',
      { status: 'pending', phase: 'downloading' },
      { type: 'failed' },
      { status: 'failed', phase: 'failed' },
    ],
    [
      'a failed re-acquisition of a ready track',
      { status: 'ready', phase: 'downloading' },
      { type: 'replaceFailed' },
      { status: 'ready', phase: 'failed' },
    ],
    [
      'a pending response over a ready status',
      { status: 'ready', phase: undefined },
      { type: 'response', status: 'pending' },
      null,
    ],
    [
      'a pending response over a failed status',
      { status: 'failed', phase: undefined },
      { type: 'response', status: 'pending' },
      null,
    ],
    [
      'a pending response with nothing held',
      NOTHING,
      { type: 'response', status: 'pending' },
      { status: 'pending', phase: undefined },
    ],
    [
      'a ready response over a pending status',
      { status: 'pending', phase: undefined },
      { type: 'response', status: 'ready' },
      { status: 'ready', phase: undefined },
    ],
  ])('%s', (_label, state, event, expected) => {
    expect(nextLifecycle(state, event)).toEqual(expected);
  });
});

describe('acquisition predicates', () => {
  it.each<[string | null | undefined, boolean, boolean, boolean, boolean]>([
    ['ready', true, false, false, false],
    ['pending', false, true, false, false],
    ['failed', false, false, true, true],
    [null, false, false, false, false],
    [undefined, false, false, false, false],
  ])(
    'for %s: ready %s, pending %s, failed %s, can retry %s',
    (status, ready, pending, failed, retry) => {
      expect([isReady(status), isPending(status), isFailed(status), canRetry(status)]).toEqual([
        ready,
        pending,
        failed,
        retry,
      ]);
    },
  );
});
