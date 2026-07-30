import { usePinnedStore, type PinnedEntry, type PinnedStatus } from '../pinnedStore';

jest.mock('@shared/api-client/audio', () => ({
  fetchAudioUrls: jest.fn().mockResolvedValue([]),
}));

function resetStore(
  overrides: Partial<{ entries: Record<string, PinnedEntry>; queue: string[]; isWorking: boolean }> = {},
): void {
  usePinnedStore.setState({ entries: {}, queue: [], isWorking: false, ...overrides });
}

async function flushAsync(): Promise<void> {
  for (let i = 0; i < 20; i += 1) {
    await Promise.resolve();
  }
}

function readyEntry(trackId: string): PinnedEntry {
  return { trackId, status: 'ready', uri: `file:///document/offline-audio/${trackId}.mp3` };
}

beforeEach(() => {
  resetStore();
});

afterEach(async () => {
  await flushAsync();
});

describe('pin — reducer matrix over every entry state', () => {
  it('a fresh trackId, absent from an empty index, advances synchronously to downloading and drains the queue', () => {
    usePinnedStore.getState().pin('t1');

    const { entries, queue, isWorking } = usePinnedStore.getState();
    expect(entries['t1']).toEqual({ trackId: 't1', status: 'downloading' });
    expect(queue).toEqual([]);
    expect(isWorking).toBe(true);
  });

  it('a trackId absent from a non-empty index enqueues it, leaving unrelated entries untouched', () => {
    resetStore({ entries: { other: readyEntry('other') } });

    usePinnedStore.getState().pin('t1');

    const { entries } = usePinnedStore.getState();
    expect(entries['t1']?.status).toBe('downloading');
    expect(entries['other']).toEqual(readyEntry('other'));
  });

  it('illegal pair: an already-queued entry is not re-enqueued, so a second tap cannot duplicate its download', () => {
    resetStore({ entries: { t1: { trackId: 't1', status: 'queued' } }, queue: ['t1'] });

    usePinnedStore.getState().pin('t1');

    expect(usePinnedStore.getState().entries['t1']).toEqual({ trackId: 't1', status: 'queued' });
    expect(usePinnedStore.getState().queue).toEqual(['t1']);
  });

  it('a failed entry is retried — the "Retry download" tap', () => {
    resetStore({ entries: { t1: { trackId: 't1', status: 'failed' } } });

    usePinnedStore.getState().pin('t1');

    expect(usePinnedStore.getState().entries['t1']?.status).toBe('downloading');
  });

  it.each<[string, PinnedEntry]>([
    ['downloading', { trackId: 't1', status: 'downloading' }],
    ['ready', readyEntry('t1')],
  ])(
    'illegal pair: a %s entry is not re-enqueued and the store is not touched at all',
    (_status, seedEntry) => {
      resetStore({ entries: { t1: seedEntry } });
      const before = usePinnedStore.getState();

      usePinnedStore.getState().pin('t1');

      expect(usePinnedStore.getState()).toBe(before);
    },
  );
});

describe('pinMany — reducer matrix, and the filter that makes "Download rest" mean something', () => {
  it('on an empty index, queues every id and synchronously advances only the first to downloading', () => {
    usePinnedStore.getState().pinMany(['t1', 't2', 't3']);

    const { entries, queue } = usePinnedStore.getState();
    expect(entries['t1']?.status).toBe('downloading');
    expect(entries['t2']?.status).toBe('queued');
    expect(entries['t3']?.status).toBe('queued');
    expect(queue).toEqual(['t2', 't3']);
  });

  it('filters out ready, downloading and already-queued ids while keeping failed and absent ids fresh', () => {
    resetStore({
      entries: {
        readyId: readyEntry('readyId'),
        dlId: { trackId: 'dlId', status: 'downloading' },
        queuedId: { trackId: 'queuedId', status: 'queued' },
        failedId: { trackId: 'failedId', status: 'failed' },
      },
    });

    usePinnedStore.getState().pinMany(['readyId', 'dlId', 'queuedId', 'failedId', 'newId']);

    const { entries, queue } = usePinnedStore.getState();
    expect(entries['readyId']).toEqual(readyEntry('readyId'));
    expect(entries['dlId']).toEqual({ trackId: 'dlId', status: 'downloading' });
    expect(entries['queuedId']).toEqual({ trackId: 'queuedId', status: 'queued' });
    expect(entries['failedId']?.status).toBe('downloading');
    expect(entries['newId']?.status).toBe('queued');
    expect(queue).toEqual(['newId']);
  });

  it('illegal pair: when every id is already ready or downloading, the store is not touched at all — "everything is downloaded"', () => {
    resetStore({
      entries: { a: readyEntry('a'), b: { trackId: 'b', status: 'downloading' } },
    });
    const before = usePinnedStore.getState();

    usePinnedStore.getState().pinMany(['a', 'b']);

    expect(usePinnedStore.getState()).toBe(before);
  });

  it('illegal pair: an empty id list touches nothing', () => {
    const before = usePinnedStore.getState();

    usePinnedStore.getState().pinMany([]);

    expect(usePinnedStore.getState()).toBe(before);
  });
});

describe('unpin — reducer matrix over every entry state', () => {
  it.each<[PinnedStatus, PinnedEntry]>([
    ['queued', { trackId: 't1', status: 'queued' }],
    ['downloading', { trackId: 't1', status: 'downloading' }],
    ['ready', readyEntry('t1')],
    ['failed', { trackId: 't1', status: 'failed' }],
  ])('removes a %s track from entries entirely', (_status, seedEntry) => {
    resetStore({ entries: { t1: seedEntry } });

    usePinnedStore.getState().unpin('t1');

    expect(usePinnedStore.getState().entries['t1']).toBeUndefined();
  });

  it('removes the track from queue when present, regardless of the status recorded against it', () => {
    resetStore({
      entries: { t1: { trackId: 't1', status: 'queued' }, t2: { trackId: 't2', status: 'queued' } },
      queue: ['t1', 't2'],
    });

    usePinnedStore.getState().unpin('t1');

    expect(usePinnedStore.getState().queue).toEqual(['t2']);
  });

  it('on an empty index, unpinning an unknown track is a safe no-op', () => {
    expect(() => usePinnedStore.getState().unpin('ghost')).not.toThrow();
    expect(usePinnedStore.getState().entries).toEqual({});
    expect(usePinnedStore.getState().queue).toEqual([]);
  });

  it('a track not present in the index does not corrupt the rest of it', () => {
    resetStore({
      entries: { keep1: readyEntry('keep1'), keep2: { trackId: 'keep2', status: 'queued' } },
      queue: ['keep2'],
    });

    usePinnedStore.getState().unpin('unknown');

    expect(usePinnedStore.getState().entries).toEqual({
      keep1: readyEntry('keep1'),
      keep2: { trackId: 'keep2', status: 'queued' },
    });
    expect(usePinnedStore.getState().queue).toEqual(['keep2']);
  });
});

describe('unpinAll — reducer matrix', () => {
  it('clears every entry and the queue regardless of mixed statuses', () => {
    resetStore({
      entries: {
        a: readyEntry('a'),
        b: { trackId: 'b', status: 'downloading' },
        c: { trackId: 'c', status: 'queued' },
        d: { trackId: 'd', status: 'failed' },
      },
      queue: ['c'],
    });

    usePinnedStore.getState().unpinAll();

    expect(usePinnedStore.getState().entries).toEqual({});
    expect(usePinnedStore.getState().queue).toEqual([]);
  });

  it('on an already-empty index, it stays empty and does not throw', () => {
    expect(() => usePinnedStore.getState().unpinAll()).not.toThrow();
    expect(usePinnedStore.getState().entries).toEqual({});
  });
});

describe('idempotence / replay — apply(apply(e)) equals apply(e)', () => {
  it('unpin twice: the second call is a no-op identical to the state left by the first', () => {
    resetStore({
      entries: { t1: readyEntry('t1'), t2: { trackId: 't2', status: 'queued' } },
      queue: ['t2'],
    });

    usePinnedStore.getState().unpin('t1');
    const afterFirst = usePinnedStore.getState();

    usePinnedStore.getState().unpin('t1');

    expect(usePinnedStore.getState().entries).toEqual(afterFirst.entries);
    expect(usePinnedStore.getState().queue).toEqual(afterFirst.queue);
  });

  it('unpinAll twice: the second call leaves the index empty, same as the first', () => {
    resetStore({ entries: { t1: readyEntry('t1') } });

    usePinnedStore.getState().unpinAll();
    usePinnedStore.getState().unpinAll();

    expect(usePinnedStore.getState().entries).toEqual({});
    expect(usePinnedStore.getState().queue).toEqual([]);
  });

  it('pin twice on an already-ready track: both calls are guarded no-ops, the entry never re-enters the queue', () => {
    const ready = readyEntry('t1');
    resetStore({ entries: { t1: ready } });

    usePinnedStore.getState().pin('t1');
    usePinnedStore.getState().pin('t1');

    expect(usePinnedStore.getState().entries['t1']).toEqual(ready);
    expect(usePinnedStore.getState().queue).toEqual([]);
  });

  it('pinMany twice over the same ids does not duplicate a still-queued id in the queue', () => {
    usePinnedStore.getState().pinMany(['t1', 't2']);
    const queueAfterFirst = [...usePinnedStore.getState().queue];

    usePinnedStore.getState().pinMany(['t1', 't2']);

    expect(usePinnedStore.getState().queue).toEqual(queueAfterFirst);
  });
});
