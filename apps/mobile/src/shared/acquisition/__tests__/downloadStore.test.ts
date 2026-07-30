import { act, renderHook } from '@testing-library/react-native';
import fc from 'fast-check';

import {
  DONE_HOLD_MS,
  FAILED_HOLD_MS,
  FINISHING_DWELL_MS,
  aggregatePhase,
  useActiveDownloadItems,
  useDownloadStore,
  type DownloadEntry,
  type DownloadPhase,
} from '../downloadStore';

function entry(trackId: string, phase: DownloadPhase): DownloadEntry {
  return { trackId, phase, title: null, artist: null, artworkUrl: null };
}

function seedPhase(trackId: string, target: DownloadPhase): void {
  useDownloadStore.getState().start(trackId);
  if (target === 'finding') return;
  useDownloadStore.getState().progress(trackId, 'downloading');
  if (target === 'downloading') return;
  useDownloadStore.getState().progress(trackId, 'finishing');
  if (target === 'finishing') return;
  useDownloadStore.getState().progress(trackId, target);
}

beforeEach(() => {
  jest.useFakeTimers();
  useDownloadStore.getState().reset();
});

afterEach(() => {
  useDownloadStore.getState().reset();
  jest.useRealTimers();
});

describe('progress', () => {
  it.each<[DownloadPhase, DownloadPhase, boolean]>([
    ['finding', 'downloading', true],
    ['finding', 'finishing', true],
    ['downloading', 'finding', false],
    ['finishing', 'downloading', false],
    ['finishing', 'done', true],
    ['done', 'failed', false],
    ['failed', 'done', false],
  ])('from %s, a %s event is accepted: %s', (from, to, accepted) => {
    seedPhase('t1', from);

    useDownloadStore.getState().progress('t1', to);

    expect(useDownloadStore.getState().entries['t1']?.phase).toBe(accepted ? to : from);
  });

  it('rejects a done event that arrives after the track already failed (both share rank 3)', () => {
    seedPhase('t1', 'failed');

    useDownloadStore.getState().progress('t1', 'done');

    expect(useDownloadStore.getState().entries['t1']?.phase).toBe('failed');
  });

  it('updates meta on a same-phase repeat instead of dropping it (rank guard is strict less-than)', () => {
    useDownloadStore.getState().start('t1', { title: 'Discovery', artist: 'Daft Punk' });
    useDownloadStore.getState().progress('t1', 'downloading', { artworkUrl: 'first.png' });

    useDownloadStore.getState().progress('t1', 'downloading', { artworkUrl: 'second.png' });

    const current = useDownloadStore.getState().entries['t1'];
    expect(current?.phase).toBe('downloading');
    expect(current?.artworkUrl).toBe('second.png');
    expect(current?.title).toBe('Discovery');
  });

  it('applying the same progress event twice is idempotent', () => {
    useDownloadStore.getState().start('t1', { title: 'Random Access Memories', artist: 'Daft Punk' });
    useDownloadStore.getState().progress('t1', 'downloading', { artworkUrl: 'cover.png' });
    const once = useDownloadStore.getState().entries['t1'];

    useDownloadStore.getState().progress('t1', 'downloading', { artworkUrl: 'cover.png' });
    const twice = useDownloadStore.getState().entries['t1'];

    expect(twice).toEqual(once);
  });

  it('lets a later progress event overwrite an existing title and artist with fresher metadata', () => {
    useDownloadStore.getState().start('t1', { title: 'Working Title', artist: 'Unknown Artist' });

    useDownloadStore
      .getState()
      .progress('t1', 'downloading', { title: 'Discovery', artist: 'Daft Punk' });

    const current = useDownloadStore.getState().entries['t1'];
    expect(current?.title).toBe('Discovery');
    expect(current?.artist).toBe('Daft Punk');
  });

  it('creates a fresh entry for a trackId with no prior start', () => {
    useDownloadStore.getState().progress('t1', 'downloading', { title: 'Homework', artist: 'Daft Punk' });

    expect(useDownloadStore.getState().entries['t1']).toEqual({
      trackId: 't1',
      phase: 'downloading',
      title: 'Homework',
      artist: 'Daft Punk',
      artworkUrl: null,
    });
  });
});

describe('complete', () => {
  it('holds finishing for the dwell window, then done for the hold window, then removes the entry', () => {
    useDownloadStore.getState().start('t1', { title: 'Voyage', artist: 'Daft Punk' });

    useDownloadStore.getState().complete('t1');
    expect(useDownloadStore.getState().entries['t1']?.phase).toBe('finishing');

    jest.advanceTimersByTime(FINISHING_DWELL_MS - 1);
    expect(useDownloadStore.getState().entries['t1']?.phase).toBe('finishing');

    jest.advanceTimersByTime(1);
    expect(useDownloadStore.getState().entries['t1']?.phase).toBe('done');

    jest.advanceTimersByTime(DONE_HOLD_MS - 1);
    expect(useDownloadStore.getState().entries['t1']?.phase).toBe('done');

    jest.advanceTimersByTime(1);
    expect(useDownloadStore.getState().entries['t1']).toBeUndefined();
  });

  it('preserves title, artist and artworkUrl through the done transition', () => {
    useDownloadStore
      .getState()
      .start('t1', { title: 'Discovery', artist: 'Daft Punk', artworkUrl: 'discovery.png' });

    useDownloadStore.getState().complete('t1');
    expect(useDownloadStore.getState().entries['t1']?.artworkUrl).toBe('discovery.png');

    jest.advanceTimersByTime(FINISHING_DWELL_MS);

    const current = useDownloadStore.getState().entries['t1'];
    expect(current?.phase).toBe('done');
    expect(current?.title).toBe('Discovery');
    expect(current?.artist).toBe('Daft Punk');
    expect(current?.artworkUrl).toBe('discovery.png');
  });

  it("does not wipe a different track's entry when one track's dwell timer removes it", () => {
    useDownloadStore.getState().start('t1');
    useDownloadStore.getState().start('t2');
    useDownloadStore.getState().complete('t1');

    jest.advanceTimersByTime(FINISHING_DWELL_MS + DONE_HOLD_MS);

    expect(useDownloadStore.getState().entries['t1']).toBeUndefined();
    expect(useDownloadStore.getState().entries['t2']).toBeDefined();
    expect(useDownloadStore.getState().entries['t2']?.phase).toBe('finding');
  });

  it("a second complete() call cancels the first call's pending done/remove timers instead of racing them", () => {
    useDownloadStore.getState().start('t1');
    useDownloadStore.getState().complete('t1');

    jest.advanceTimersByTime(100);
    useDownloadStore.getState().complete('t1');

    // One tick PAST the FIRST call's removal timer (scheduled from t=0), but well before
    // the SECOND call's own removal timer (scheduled from t=100). If complete() failed to
    // cancel the first call's timers, the stale removal fires here and the entry is gone.
    jest.advanceTimersByTime(FINISHING_DWELL_MS + DONE_HOLD_MS - 100 + 1);
    expect(useDownloadStore.getState().entries['t1']).toBeDefined();

    // Past the SECOND call's own removal timer.
    jest.advanceTimersByTime(100);
    expect(useDownloadStore.getState().entries['t1']).toBeUndefined();
  });

  it('cancels a pending removal timer when the track restarts mid-dwell', () => {
    useDownloadStore.getState().start('t1');
    useDownloadStore.getState().complete('t1');

    jest.advanceTimersByTime(100);
    useDownloadStore.getState().start('t1');

    jest.advanceTimersByTime(FINISHING_DWELL_MS + DONE_HOLD_MS);

    const current = useDownloadStore.getState().entries['t1'];
    expect(current).toBeDefined();
    expect(current?.phase).toBe('finding');
  });
});

describe('fail', () => {
  it('preserves title, artist and artworkUrl through the failed transition', () => {
    useDownloadStore
      .getState()
      .start('t1', { title: 'Homework', artist: 'Daft Punk', artworkUrl: 'homework.png' });

    useDownloadStore.getState().fail('t1');

    const current = useDownloadStore.getState().entries['t1'];
    expect(current?.phase).toBe('failed');
    expect(current?.title).toBe('Homework');
    expect(current?.artist).toBe('Daft Punk');
    expect(current?.artworkUrl).toBe('homework.png');
  });

  it("cancels a pending complete() done timer, so a track that fails mid-finishing doesn't flip back to done", () => {
    useDownloadStore.getState().start('t1');
    useDownloadStore.getState().complete('t1'); // schedules done@+FINISHING_DWELL_MS, remove@+that+DONE_HOLD_MS

    jest.advanceTimersByTime(100);
    useDownloadStore.getState().fail('t1');

    // Reach the exact instant complete()'s stale "done" timer would have fired.
    jest.advanceTimersByTime(FINISHING_DWELL_MS - 100);
    expect(useDownloadStore.getState().entries['t1']?.phase).toBe('failed');

    // Reach the exact instant complete()'s stale "remove" timer would have fired.
    jest.advanceTimersByTime(DONE_HOLD_MS);
    expect(useDownloadStore.getState().entries['t1']?.phase).toBe('failed');

    // fail()'s own removal timer, scheduled FAILED_HOLD_MS after the fail() call.
    jest.advanceTimersByTime(FAILED_HOLD_MS);
    expect(useDownloadStore.getState().entries['t1']).toBeUndefined();
  });

  it('creates a failed entry for a trackId that never called start (a late-connecting client)', () => {
    useDownloadStore.getState().fail('unknown-track');

    const current = useDownloadStore.getState().entries['unknown-track'];
    expect(current).toBeDefined();
    expect(current?.phase).toBe('failed');
    expect(current?.title).toBeNull();
    expect(current?.artist).toBeNull();
  });

  it('removes the entry after the failed hold window, not before', () => {
    useDownloadStore.getState().fail('t1');

    jest.advanceTimersByTime(FAILED_HOLD_MS - 1);
    expect(useDownloadStore.getState().entries['t1']).toBeDefined();

    jest.advanceTimersByTime(1);
    expect(useDownloadStore.getState().entries['t1']).toBeUndefined();
  });
});

describe('reset', () => {
  it("cancels pending timers, so a stale done/remove callback can't corrupt a track restarted after reset", () => {
    useDownloadStore.getState().start('t1');
    useDownloadStore.getState().complete('t1'); // schedules done@+FINISHING_DWELL_MS, remove@+that+DONE_HOLD_MS

    jest.advanceTimersByTime(100);
    useDownloadStore.getState().reset();
    useDownloadStore.getState().start('t1');

    jest.advanceTimersByTime(FINISHING_DWELL_MS + DONE_HOLD_MS);

    const current = useDownloadStore.getState().entries['t1'];
    expect(current).toBeDefined();
    expect(current?.phase).toBe('finding');
  });
});

describe('useActiveDownloadItems', () => {
  it('filters out failed entries and sorts the rest by trackId', () => {
    seedPhase('zebra', 'finding');
    seedPhase('alpha', 'downloading');
    seedPhase('middle', 'finishing');
    seedPhase('delta', 'done');
    seedPhase('omega', 'failed');

    const { result } = renderHook(() => useActiveDownloadItems());

    expect(result.current.map((e) => e.trackId)).toEqual(['alpha', 'delta', 'middle', 'zebra']);
    expect(result.current.map((e) => e.phase)).toEqual([
      'downloading',
      'done',
      'finishing',
      'finding',
    ]);
  });

  it('reflects a live phase change after mount without a remount', () => {
    useDownloadStore.getState().start('t1');
    const { result } = renderHook(() => useActiveDownloadItems());

    expect(result.current.map((e) => e.phase)).toEqual(['finding']);

    act(() => {
      useDownloadStore.getState().progress('t1', 'downloading');
    });

    expect(result.current.map((e) => e.phase)).toEqual(['downloading']);
  });
});

describe('aggregatePhase', () => {
  it('returns undefined for an empty list', () => {
    expect(aggregatePhase([])).toBeUndefined();
  });

  it('returns done when every item is done', () => {
    const items = [entry('a', 'done'), entry('b', 'done')];
    expect(aggregatePhase(items)).toBe('done');
  });

  it('returns the minimum-rank phase among non-done items, ignoring done entries', () => {
    const items = [entry('a', 'done'), entry('b', 'finishing'), entry('c', 'finding')];
    expect(aggregatePhase(items)).toBe('finding');
  });

  it('treats failed as an active (non-done) phase competing on rank', () => {
    const items = [entry('a', 'failed'), entry('b', 'downloading')];
    expect(aggregatePhase(items)).toBe('downloading');
  });

  it('returns failed when it is the only non-done phase left', () => {
    const items = [entry('a', 'done'), entry('b', 'failed')];
    expect(aggregatePhase(items)).toBe('failed');
  });

  it('is order-independent over a fixed multiset of phases', () => {
    const phases: DownloadPhase[] = [
      'finding',
      'downloading',
      'finishing',
      'done',
      'failed',
      'downloading',
    ];
    const baseline = aggregatePhase(phases.map((phase, i) => entry(`t${i}`, phase)));

    fc.assert(
      fc.property(
        fc.shuffledSubarray(phases, { minLength: phases.length, maxLength: phases.length }),
        (shuffled) => {
          const items = shuffled.map((phase, i) => entry(`t${i}`, phase));
          expect(aggregatePhase(items)).toBe(baseline);
        },
      ),
    );
  });
});
