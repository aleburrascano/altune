import { act } from '@testing-library/react-native';
import * as FileSystem from 'expo-file-system';
import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';
import { asTrackId, type TrackId } from '@shared/api-client/ids';
import {
  createMemoryFileStore,
  type MemoryFileStore,
} from '@shared/files/__tests__/memoryFileStore';
import type { StoredDirectory, StoredFile } from '@shared/files/fileStore';
import { applyKillSwitches, setKillSwitchFileStore } from '@shared/killSwitch/killSwitch';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';
import {
  setPinnedFileStore,
  MAX_PINNED_BYTES,
  MIN_FREE_BYTES,
  PIN_DOWNLOAD_TIMEOUT_MS,
} from '../pinnedFiles';
import {
  INDEX_WRITE_DELAY_MS,
  flushIndex,
  loadIndex,
  readOwner,
  saveIndex,
  scheduleSaveIndex,
  setPinnedIndexFileStore,
  writeOwner,
} from '../pinnedIndex';
import {
  usePinnedStore,
  type PinnedEntry,
  type PinnedStatus,
  resolvePinnedUri,
  repinIfPinned,
  claimPinnedDownloads,
} from '../pinnedStore';

// One shared mock for the whole file: jest hoists jest.mock per file, so every group below sees
// this same fetchAudioUrls. The default resolves no urls; a group that needs another behaviour
// resets and configures it in its own beforeEach.
jest.mock('@shared/api-client/audio', () => ({
  fetchAudioUrls: jest.fn().mockResolvedValue([]),
}));

const fetchAudioUrlsMock = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

beforeEach(() => {
  fetchAudioUrlsMock.mockReset();
  fetchAudioUrlsMock.mockResolvedValue([]);
});

describe('store actions: pin, pinMany, unpin and unpinAll across every entry state', () => {
  function resetStore(
    overrides: Partial<{
      entries: Record<string, PinnedEntry>;
      queue: TrackId[];
      isWorking: boolean;
    }> = {},
  ): void {
    usePinnedStore.setState({ entries: {}, queue: [], isWorking: false, ...overrides });
  }

  async function flushAsync(): Promise<void> {
    for (let i = 0; i < 20; i += 1) {
      await Promise.resolve();
    }
  }

  function readyEntry(trackId: string): PinnedEntry {
    return {
      trackId: trackId as TrackId,
      status: 'ready',
      uri: `file:///document/offline-audio/${trackId}.mp3`,
    };
  }

  beforeEach(() => {
    resetStore();
  });

  afterEach(async () => {
    await flushAsync();
  });

  describe('pin — reducer matrix over every entry state', () => {
    it('a fresh trackId, absent from an empty index, advances synchronously to downloading and drains the queue', () => {
      usePinnedStore.getState().pin(asTrackId('t1'));

      const { entries, queue, isWorking } = usePinnedStore.getState();
      expect(entries['t1']).toEqual({ trackId: asTrackId('t1'), status: 'downloading' });
      expect(queue).toEqual([]);
      expect(isWorking).toBe(true);
    });

    it('a trackId absent from a non-empty index enqueues it, leaving unrelated entries untouched', () => {
      resetStore({ entries: { other: readyEntry('other') } });

      usePinnedStore.getState().pin(asTrackId('t1'));

      const { entries } = usePinnedStore.getState();
      expect(entries['t1']?.status).toBe('downloading');
      expect(entries['other']).toEqual(readyEntry('other'));
    });

    it('illegal pair: an already-queued entry is not re-enqueued, so a second tap cannot duplicate its download', () => {
      resetStore({
        entries: { t1: { trackId: asTrackId('t1'), status: 'queued' } },
        queue: [asTrackId('t1')],
      });

      usePinnedStore.getState().pin(asTrackId('t1'));

      expect(usePinnedStore.getState().entries['t1']).toEqual({
        trackId: asTrackId('t1'),
        status: 'queued',
      });
      expect(usePinnedStore.getState().queue).toEqual(['t1']);
    });

    it('a failed entry is retried — the "Retry download" tap', () => {
      resetStore({ entries: { t1: { trackId: asTrackId('t1'), status: 'failed' } } });

      usePinnedStore.getState().pin(asTrackId('t1'));

      expect(usePinnedStore.getState().entries['t1']?.status).toBe('downloading');
    });

    it.each<[string, PinnedEntry]>([
      ['downloading', { trackId: asTrackId('t1'), status: 'downloading' }],
      ['ready', readyEntry('t1')],
    ])(
      'illegal pair: a %s entry is not re-enqueued and the store is not touched at all',
      (_status, seedEntry) => {
        resetStore({ entries: { t1: seedEntry } });
        const before = usePinnedStore.getState();

        usePinnedStore.getState().pin(asTrackId('t1'));

        expect(usePinnedStore.getState()).toBe(before);
      },
    );
  });

  describe('pinMany — reducer matrix, and the filter that makes "Download rest" mean something', () => {
    it('on an empty index, queues every id and synchronously advances only the first to downloading', () => {
      usePinnedStore.getState().pinMany([asTrackId('t1'), asTrackId('t2'), asTrackId('t3')]);

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
          dlId: { trackId: asTrackId('dlId'), status: 'downloading' },
          queuedId: { trackId: asTrackId('queuedId'), status: 'queued' },
          failedId: { trackId: asTrackId('failedId'), status: 'failed' },
        },
      });

      usePinnedStore
        .getState()
        .pinMany([
          asTrackId('readyId'),
          asTrackId('dlId'),
          asTrackId('queuedId'),
          asTrackId('failedId'),
          asTrackId('newId'),
        ]);

      const { entries, queue } = usePinnedStore.getState();
      expect(entries['readyId']).toEqual(readyEntry('readyId'));
      expect(entries['dlId']).toEqual({ trackId: asTrackId('dlId'), status: 'downloading' });
      expect(entries['queuedId']).toEqual({ trackId: asTrackId('queuedId'), status: 'queued' });
      expect(entries['failedId']?.status).toBe('downloading');
      expect(entries['newId']?.status).toBe('queued');
      expect(queue).toEqual(['newId']);
    });

    it('illegal pair: when every id is already ready or downloading, the store is not touched at all — "everything is downloaded"', () => {
      resetStore({
        entries: { a: readyEntry('a'), b: { trackId: asTrackId('b'), status: 'downloading' } },
      });
      const before = usePinnedStore.getState();

      usePinnedStore.getState().pinMany([asTrackId('a'), asTrackId('b')]);

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
      ['queued', { trackId: asTrackId('t1'), status: 'queued' }],
      ['downloading', { trackId: asTrackId('t1'), status: 'downloading' }],
      ['ready', readyEntry('t1')],
      ['failed', { trackId: asTrackId('t1'), status: 'failed' }],
    ])('removes a %s track from entries entirely', (_status, seedEntry) => {
      resetStore({ entries: { t1: seedEntry } });

      usePinnedStore.getState().unpin(asTrackId('t1'));

      expect(usePinnedStore.getState().entries['t1']).toBeUndefined();
    });

    it('removes the track from queue when present, regardless of the status recorded against it', () => {
      resetStore({
        entries: {
          t1: { trackId: asTrackId('t1'), status: 'queued' },
          t2: { trackId: asTrackId('t2'), status: 'queued' },
        },
        queue: [asTrackId('t1'), asTrackId('t2')],
      });

      usePinnedStore.getState().unpin(asTrackId('t1'));

      expect(usePinnedStore.getState().queue).toEqual(['t2']);
    });

    it('on an empty index, unpinning an unknown track is a safe no-op', () => {
      expect(() => usePinnedStore.getState().unpin(asTrackId('ghost'))).not.toThrow();
      expect(usePinnedStore.getState().entries).toEqual({});
      expect(usePinnedStore.getState().queue).toEqual([]);
    });

    it('a track not present in the index does not corrupt the rest of it', () => {
      resetStore({
        entries: {
          keep1: readyEntry('keep1'),
          keep2: { trackId: asTrackId('keep2'), status: 'queued' },
        },
        queue: [asTrackId('keep2')],
      });

      usePinnedStore.getState().unpin(asTrackId('unknown'));

      expect(usePinnedStore.getState().entries).toEqual({
        keep1: readyEntry('keep1'),
        keep2: { trackId: asTrackId('keep2'), status: 'queued' },
      });
      expect(usePinnedStore.getState().queue).toEqual(['keep2']);
    });
  });

  describe('unpinAll — reducer matrix', () => {
    it('clears every entry and the queue regardless of mixed statuses', () => {
      resetStore({
        entries: {
          a: readyEntry('a'),
          b: { trackId: asTrackId('b'), status: 'downloading' },
          c: { trackId: asTrackId('c'), status: 'queued' },
          d: { trackId: asTrackId('d'), status: 'failed' },
        },
        queue: [asTrackId('c')],
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
        entries: { t1: readyEntry('t1'), t2: { trackId: asTrackId('t2'), status: 'queued' } },
        queue: [asTrackId('t2')],
      });

      usePinnedStore.getState().unpin(asTrackId('t1'));
      const afterFirst = usePinnedStore.getState();

      usePinnedStore.getState().unpin(asTrackId('t1'));

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

      usePinnedStore.getState().pin(asTrackId('t1'));
      usePinnedStore.getState().pin(asTrackId('t1'));

      expect(usePinnedStore.getState().entries['t1']).toEqual(ready);
      expect(usePinnedStore.getState().queue).toEqual([]);
    });

    it('pinMany twice over the same ids does not duplicate a still-queued id in the queue', () => {
      usePinnedStore.getState().pinMany([asTrackId('t1'), asTrackId('t2')]);
      const queueAfterFirst = [...usePinnedStore.getState().queue];

      usePinnedStore.getState().pinMany([asTrackId('t1'), asTrackId('t2')]);

      expect(usePinnedStore.getState().queue).toEqual(queueAfterFirst);
    });
  });
});

describe('pinMany reports a per-batch result', () => {
  function resolved(trackId: string): ResolvedAudioUrl {
    return {
      trackId,
      url: `https://cdn.example.com/audio/${trackId}.mp3?sig=t&exp=999`,
      version: 'v1',
    };
  }

  async function flush(rounds = 40): Promise<void> {
    for (let i = 0; i < rounds; i += 1) {
      await Promise.resolve();
    }
  }

  beforeEach(() => {
    usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
    fetchAudioUrlsMock.mockReset();
  });

  describe('pinMany — per-batch result', () => {
    it('resolves once every track in the batch settles, counting the failed ones', async () => {
      fetchAudioUrlsMock.mockImplementation(async ([id]) => {
        if (id === 'B') throw new Error('404');
        return [resolved(id!)];
      });

      let result: unknown;
      await act(async () => {
        result = await usePinnedStore
          .getState()
          .pinMany([asTrackId('A'), asTrackId('B'), asTrackId('C')]);
      });

      expect(result).toEqual({ requested: 3, failed: 1 });
      expect(usePinnedStore.getState().entries['B']?.status).toBe('failed');
    });

    it('stays pending while a batch track is still downloading', async () => {
      let finish!: (urls: ResolvedAudioUrl[]) => void;
      fetchAudioUrlsMock.mockImplementation(
        () => new Promise<ResolvedAudioUrl[]>((res) => (finish = res)),
      );

      let settled = false;
      const batch = usePinnedStore.getState().pinMany([asTrackId('A')]);
      void batch.then(() => (settled = true));
      await act(async () => flush());
      expect(settled).toBe(false);

      await act(async () => {
        finish([resolved('A')]);
        await flush();
      });
      expect(settled).toBe(true);
      await expect(batch).resolves.toEqual({ requested: 1, failed: 0 });
    });

    it('only counts tracks the batch actually queued, and a track unpinned mid-batch is not a failure', async () => {
      usePinnedStore.setState({
        entries: {
          A: { trackId: asTrackId('A'), status: 'ready', uri: 'file:///a', version: 'v1' },
        },
      });
      let finish!: (urls: ResolvedAudioUrl[]) => void;
      fetchAudioUrlsMock.mockImplementation(
        () => new Promise<ResolvedAudioUrl[]>((res) => (finish = res)),
      );

      const batch = usePinnedStore.getState().pinMany([asTrackId('A'), asTrackId('B')]);
      await act(async () => {
        usePinnedStore.getState().unpin(asTrackId('B'));
        finish([resolved('B')]);
        await flush();
      });

      await expect(batch).resolves.toEqual({ requested: 1, failed: 0 });
    });

    it('resolves an empty result straight away when nothing needs downloading', async () => {
      await expect(usePinnedStore.getState().pinMany([])).resolves.toEqual({
        requested: 0,
        failed: 0,
      });
    });
  });
});

describe('the pinned index is written through to disk and loaded back on relaunch', () => {
  type FsFailureKind = 'write' | 'read' | 'delete' | 'download' | 'createDirectory' | 'list';

  const { __fs } = FileSystem as unknown as {
    __fs: {
      reset(): void;
      seedFile(uri: string, contents: string): void;
      readFile(uri: string): string | undefined;
      allFiles(): Record<string, string>;
      failNext(kind: FsFailureKind, error?: Error): void;
    };
  };

  const INDEX_URI = 'file:///document/offline/pinned.json';
  const AUDIO_DIR = 'file:///document/offline-audio';

  function audioUri(trackId: string): string {
    return `${AUDIO_DIR}/${trackId}.mp3`;
  }

  function readIndex(): Record<string, PinnedEntry> {
    const raw = __fs.readFile(INDEX_URI);
    if (raw === undefined) throw new Error('pinned.json was never written');
    return (JSON.parse(raw) as { entries: Record<string, PinnedEntry> }).entries;
  }

  function readyEntry(trackId: string): PinnedEntry {
    return {
      trackId: trackId as TrackId,
      status: 'ready',
      uri: `file:///document/offline-audio/${trackId}.mp3`,
    };
  }

  async function flushAsync(): Promise<void> {
    for (let i = 0; i < 20; i += 1) {
      await Promise.resolve();
    }
  }

  beforeEach(() => {
    usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
  });

  afterEach(async () => {
    await flushAsync();
  });

  describe('pin — writes through to disk, not just to memory', () => {
    it('persists the queued entry synchronously; the downloading transition only rides the coalesced write', () => {
      usePinnedStore.getState().pin(asTrackId('t1'));

      expect(usePinnedStore.getState().entries['t1']?.status).toBe('downloading');
      expect(readIndex()).toEqual({ t1: { trackId: asTrackId('t1'), status: 'queued' } });
    });

    it('persists the queued entry itself when a download is already in flight, so no mark follows to write it', () => {
      usePinnedStore.setState({ entries: {}, queue: [], isWorking: true });

      usePinnedStore.getState().pin(asTrackId('t1'));

      expect(readIndex()).toEqual({ t1: { trackId: asTrackId('t1'), status: 'queued' } });
    });
  });

  describe('pinMany — writes through to disk, not just to memory', () => {
    it('the on-disk index matches in-memory state exactly once the batch drains, for every fresh id', async () => {
      await usePinnedStore.getState().pinMany([asTrackId('t1'), asTrackId('t2')]);
      await flushAsync();

      expect(usePinnedStore.getState().isWorking).toBe(false);
      expect(readIndex()).toEqual(usePinnedStore.getState().entries);
    });

    it('persists every queued id of a batch when a download is already in flight, so no mark follows to write them', () => {
      usePinnedStore.setState({ entries: {}, queue: [], isWorking: true });

      usePinnedStore.getState().pinMany([asTrackId('t1'), asTrackId('t2')]);

      expect(readIndex()).toEqual({
        t1: { trackId: asTrackId('t1'), status: 'queued' },
        t2: { trackId: asTrackId('t2'), status: 'queued' },
      });
    });
  });

  describe('unpin — writes the removal through to disk', () => {
    it('a removed track is gone from the on-disk index, and its sibling survives untouched', () => {
      const seed = { t1: readyEntry('t1'), t2: readyEntry('t2') };
      __fs.seedFile(INDEX_URI, JSON.stringify(seed));
      usePinnedStore.setState({ entries: seed, queue: [], isWorking: false });

      usePinnedStore.getState().unpin(asTrackId('t1'));

      expect(readIndex()).toEqual({ t2: seed.t2 });
    });
  });

  describe('unpinAll — writes an empty index to disk', () => {
    it('the on-disk index becomes exactly {}, not merely emptied in memory', () => {
      const seed = { t1: { trackId: asTrackId('t1'), status: 'queued' as const } };
      __fs.seedFile(INDEX_URI, JSON.stringify(seed));
      usePinnedStore.setState({ entries: seed, queue: [asTrackId('t1')], isWorking: false });

      usePinnedStore.getState().unpinAll();

      expect(readIndex()).toEqual({});
    });

    it('deletes every pinned track file from disk too, not just the index entries — the sign-out guarantee', () => {
      __fs.seedFile(audioUri('t1'), 'audio-bytes-1');
      __fs.seedFile(audioUri('t2'), 'audio-bytes-2');
      const seed = { t1: readyEntry('t1'), t2: readyEntry('t2') };
      usePinnedStore.setState({ entries: seed, queue: [], isWorking: false });

      usePinnedStore.getState().unpinAll();

      const remaining = __fs.allFiles();
      expect(remaining[audioUri('t1')]).toBeUndefined();
      expect(remaining[audioUri('t2')]).toBeUndefined();
    });

    it('also deletes a stray file already on disk for a track that is still mid-download when sign-out fires', () => {
      __fs.seedFile(audioUri('t3'), 'partial-bytes-from-an-in-flight-download');
      usePinnedStore.setState({
        entries: { t3: { trackId: asTrackId('t3'), status: 'downloading' } },
        queue: [],
        isWorking: true,
      });

      usePinnedStore.getState().unpinAll();

      expect(__fs.allFiles()[audioUri('t3')]).toBeUndefined();
    });
  });

  describe('index write failure — logged with context, never a bare swallow, never a throw', () => {
    it('a failed saveIndex write logs and degrades to in-memory rather than throwing', () => {
      const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
      __fs.failNext('write', new Error('disk full'));

      expect(() => usePinnedStore.getState().pin(asTrackId('t1'))).not.toThrow();

      expect(warn).toHaveBeenCalledWith(
        '[offline] failed to persist pinned index; keeping in-memory only',
      );
      expect(usePinnedStore.getState().entries['t1']).toBeDefined();
      warn.mockRestore();
    });
  });

  describe('relaunch — what one session persists is exactly what a fresh module import loads back', () => {
    it('a session left with entries queued and downloading survives a fresh import unchanged', () => {
      usePinnedStore.getState().pin(asTrackId('t1'));
      usePinnedStore.getState().pinMany([asTrackId('t2'), asTrackId('t3')]);
      const persistedBeforeRelaunch = readIndex();

      let reloadedEntries: Record<string, PinnedEntry> | undefined;
      jest.isolateModules(() => {
        const mod = require('../pinnedStore') as { usePinnedStore: typeof usePinnedStore };
        reloadedEntries = mod.usePinnedStore.getState().entries;
      });

      expect(reloadedEntries).toEqual(persistedBeforeRelaunch);
    });

    it('a session left fully unpinned survives a fresh import as an empty index', () => {
      usePinnedStore.getState().pin(asTrackId('t1'));
      usePinnedStore.getState().unpin(asTrackId('t1'));

      let reloadedEntries: Record<string, PinnedEntry> | undefined;
      jest.isolateModules(() => {
        const mod = require('../pinnedStore') as { usePinnedStore: typeof usePinnedStore };
        reloadedEntries = mod.usePinnedStore.getState().entries;
      });

      expect(reloadedEntries).toEqual({});
    });

    it('a ready entry keeps its recorded audio version across a fresh import', () => {
      const seed = {
        t1: { trackId: asTrackId('t1'), status: 'ready', uri: audioUri('t1'), version: 'v3' },
      };
      __fs.seedFile(INDEX_URI, JSON.stringify(seed));

      let reloadedEntries: Record<string, PinnedEntry> | undefined;
      jest.isolateModules(() => {
        const mod = require('../pinnedStore') as { usePinnedStore: typeof usePinnedStore };
        reloadedEntries = mod.usePinnedStore.getState().entries;
      });

      expect(reloadedEntries?.['t1']).toEqual({
        trackId: asTrackId('t1'),
        status: 'ready',
        uri: audioUri('t1'),
        version: 'v3',
      });
    });
  });
});

describe('loading, validating and coalescing writes of the pinned index', () => {
  type FsFailureKind = 'write' | 'read' | 'delete' | 'download' | 'createDirectory';

  const { __fs } = FileSystem as unknown as {
    __fs: {
      reset(): void;
      seedDirectory(uri: string): void;
      seedFile(uri: string, contents: string): void;
      readFile(uri: string): string | undefined;
      allFiles(): Record<string, string>;
      failNext(kind: FsFailureKind, error?: Error): void;
    };
  };

  const INDEX_URI = 'file:///document/offline/pinned.json';
  const AUDIO_DIR_URI = 'file:///document/offline-audio';

  function readyEntry(trackId: string): Extract<PinnedEntry, { status: 'ready' }> {
    return { trackId: trackId as TrackId, status: 'ready', uri: `${AUDIO_DIR_URI}/${trackId}.mp3` };
  }

  function importFreshEntries(): unknown {
    let entries: unknown;
    jest.isolateModules(() => {
      const mod = require('../pinnedStore') as { usePinnedStore: typeof usePinnedStore };
      entries = mod.usePinnedStore.getState().entries;
    });
    return entries;
  }

  beforeEach(() => {
    usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
  });

  describe('loadIndex — legacy pinned.json shapes must not crash app launch', () => {
    it('no file at all yields an empty index', () => {
      expect(() => importFreshEntries()).not.toThrow();
      expect(importFreshEntries()).toEqual({});
    });

    it('an empty file yields an empty index instead of throwing out of JSON.parse', () => {
      __fs.seedFile(INDEX_URI, '');

      expect(() => importFreshEntries()).not.toThrow();
      expect(importFreshEntries()).toEqual({});
    });

    it('a literal null yields an empty index', () => {
      __fs.seedFile(INDEX_URI, 'null');

      expect(importFreshEntries()).toEqual({});
    });

    it('a bare string yields an empty index', () => {
      __fs.seedFile(INDEX_URI, '"just a string"');

      expect(importFreshEntries()).toEqual({});
    });

    it('a bare number yields an empty index', () => {
      __fs.seedFile(INDEX_URI, '42');

      expect(importFreshEntries()).toEqual({});
    });

    it('truncated/invalid JSON yields an empty index via the catch block', () => {
      __fs.seedFile(INDEX_URI, '{"t1": {"status": "read');

      expect(() => importFreshEntries()).not.toThrow();
      expect(importFreshEntries()).toEqual({});
    });

    it('a JSON array is rejected by the shape guard rather than loaded as an index keyed by array position', () => {
      __fs.seedFile(INDEX_URI, '["legacy-array-entry"]');

      expect(() => importFreshEntries()).not.toThrow();
      expect(importFreshEntries()).toEqual({});
    });

    // #1766: a ready entry names the file it downloaded, so one with no uri names nothing playable
    // and is as malformed as one whose uri is the wrong type.
    it('a ready entry with no uri field is dropped rather than seeded as a download naming no file', () => {
      __fs.seedFile(
        INDEX_URI,
        JSON.stringify({ t1: { trackId: asTrackId('t1'), status: 'ready' } }),
      );

      expect(importFreshEntries()).toEqual({});
    });

    it('a failed entry loads back, so a download that failed before the relaunch still offers a retry', () => {
      __fs.seedFile(
        INDEX_URI,
        JSON.stringify({ t1: { trackId: asTrackId('t1'), status: 'failed' } }),
      );

      expect(importFreshEntries()).toEqual({ t1: { trackId: asTrackId('t1'), status: 'failed' } });
    });

    it('an entry whose status this version does not know is dropped rather than seeded into store state', () => {
      __fs.seedFile(
        INDEX_URI,
        JSON.stringify({ t1: { trackId: asTrackId('t1'), status: 'archived' } }),
      );

      expect(importFreshEntries()).toEqual({});
    });

    it('an entry whose uri is the wrong type is dropped rather than seeded', () => {
      __fs.seedFile(
        INDEX_URI,
        JSON.stringify({ t1: { trackId: asTrackId('t1'), status: 'ready', uri: 42 } }),
      );

      expect(importFreshEntries()).toEqual({});
    });

    it('an entry whose version is the wrong type is dropped rather than seeded', () => {
      __fs.seedFile(
        INDEX_URI,
        JSON.stringify({
          t1: {
            trackId: asTrackId('t1'),
            status: 'ready',
            uri: `${AUDIO_DIR_URI}/t1.mp3`,
            version: 7,
          },
        }),
      );

      expect(importFreshEntries()).toEqual({});
    });

    it('an entry with no status field is dropped rather than seeded', () => {
      __fs.seedFile(INDEX_URI, JSON.stringify({ t1: { trackId: asTrackId('t1') } }));

      expect(importFreshEntries()).toEqual({});
    });

    it('a null entry value is dropped without discarding a valid sibling', () => {
      __fs.seedFile(
        INDEX_URI,
        JSON.stringify({
          bad: null,
          good: { trackId: asTrackId('good'), status: 'ready', uri: `${AUDIO_DIR_URI}/good.mp3` },
        }),
      );

      expect(importFreshEntries()).toEqual({
        good: { trackId: asTrackId('good'), status: 'ready', uri: `${AUDIO_DIR_URI}/good.mp3` },
      });
    });

    it('an entry whose trackId field is not a valid TrackId is dropped without discarding a valid sibling', () => {
      __fs.seedFile(
        INDEX_URI,
        JSON.stringify({
          bad: { trackId: '../escape', status: 'ready' },
          good: { trackId: 'good', status: 'queued' },
        }),
      );

      expect(importFreshEntries()).toEqual({ good: { trackId: 'good', status: 'queued' } });
    });

    // #1770: bracket-assigning the loaded index under this key runs Object.prototype's accessor
    // instead of defining an own property, so the entry disappears from Object.keys and the index
    // object carries a replaced prototype from launch on.
    it('an entry keyed by __proto__ is dropped, leaving the index prototype intact and a valid sibling loaded', () => {
      __fs.seedFile(
        INDEX_URI,
        '{"__proto__":{"trackId":"t1","status":"ready"},"good":{"trackId":"good","status":"queued"}}',
      );

      const entries = importFreshEntries() as Record<string, PinnedEntry>;

      expect(Object.getPrototypeOf(entries)).toBe(Object.prototype);
      expect(entries).toEqual({ good: { trackId: 'good', status: 'queued' } });
      expect(Object.keys(entries)).toEqual(['good']);
    });

    it('an entry whose map key and trackId field disagree is loaded under the map key verbatim', () => {
      __fs.seedFile(
        INDEX_URI,
        JSON.stringify({
          t1: { trackId: asTrackId('t2'), status: 'ready', uri: `${AUDIO_DIR_URI}/t2.mp3` },
        }),
      );

      const entries = importFreshEntries() as Record<string, PinnedEntry>;
      expect(entries['t1']?.trackId).toBe('t2');
      expect(entries['t2']).toBeUndefined();
    });
  });

  describe('adversarial — a shape the types promise cannot exist', () => {
    function setRawEntries(raw: unknown): void {
      usePinnedStore.setState({
        entries: raw as Record<string, PinnedEntry>,
        queue: [],
        isWorking: false,
      });
    }

    it('a null entry does not crash resolvePinnedUri — returns undefined rather than throwing on entry.status', () => {
      setRawEntries({ t1: null });

      expect(() => resolvePinnedUri(asTrackId('t1'))).not.toThrow();
      expect(resolvePinnedUri(asTrackId('t1'))).toBeUndefined();
    });

    it('a string in place of an entry object does not crash resolvePinnedUri', () => {
      setRawEntries({ t1: 'ready' });

      expect(resolvePinnedUri(asTrackId('t1'))).toBeUndefined();
    });

    it('an entry missing the status field entirely does not crash resolvePinnedUri', () => {
      setRawEntries({ t1: { trackId: asTrackId('t1'), uri: `${AUDIO_DIR_URI}/t1.mp3` } });

      expect(resolvePinnedUri(asTrackId('t1'))).toBeUndefined();
    });

    it('unpin removes a null entry without throwing, leaving a real neighbor entry intact', () => {
      setRawEntries({ t1: null, t2: readyEntry('t2') });

      expect(() => usePinnedStore.getState().unpin(asTrackId('t1'))).not.toThrow();
      expect(usePinnedStore.getState().entries['t1']).toBeUndefined();
      expect(usePinnedStore.getState().entries['t2']).toEqual(readyEntry('t2'));
    });

    describe('the empty-string key', () => {
      it('pin("") creates and reads back a distinct entry under the empty-string key', () => {
        usePinnedStore.getState().pin('' as TrackId);

        expect(usePinnedStore.getState().entries['']).toEqual({
          trackId: '',
          status: 'downloading',
        });
      });

      it('resolvePinnedUri("") returns the uri for a ready entry stored under the empty-string key', () => {
        setRawEntries({ '': readyEntry('') });

        expect(resolvePinnedUri('' as TrackId)).toBe(readyEntry('').uri);
      });

      it('unpin("") removes only the empty-string entry, leaving a same-shaped real id untouched', () => {
        setRawEntries({ '': readyEntry(''), t1: readyEntry('t1') });

        usePinnedStore.getState().unpin('' as TrackId);

        expect(usePinnedStore.getState().entries['']).toBeUndefined();
        expect(usePinnedStore.getState().entries['t1']).toBeDefined();
      });
    });

    describe('a very large index', () => {
      function buildLargeIndex(size: number): Record<string, PinnedEntry> {
        const entries: Record<string, PinnedEntry> = {};
        for (let i = 0; i < size; i += 1) {
          entries[`t${i}`] = {
            trackId: asTrackId(`t${i}`),
            status: i % 2 === 0 ? 'ready' : 'failed',
            uri: `${AUDIO_DIR_URI}/t${i}.mp3`,
          };
        }
        return entries;
      }

      it('unpinAll clears every one of 5000 entries', () => {
        usePinnedStore.setState({ entries: buildLargeIndex(5000), queue: [], isWorking: false });

        usePinnedStore.getState().unpinAll();

        expect(Object.keys(usePinnedStore.getState().entries)).toHaveLength(0);
      });

      it('resolvePinnedUri finds the right entry in a 5000-entry index without confusing neighbors', () => {
        usePinnedStore.setState({ entries: buildLargeIndex(5000), queue: [], isWorking: false });

        expect(resolvePinnedUri(asTrackId('t2500'))).toBe(`${AUDIO_DIR_URI}/t2500.mp3`);
        expect(resolvePinnedUri(asTrackId('t2501'))).toBeUndefined();
      });
    });
  });

  describe('failure injection', () => {
    it('a write failure leaves memory ahead of disk — a relaunch would resurrect the removed track', () => {
      const priorOnDisk = { t1: readyEntry('t1') };
      __fs.seedFile(INDEX_URI, JSON.stringify(priorOnDisk));
      usePinnedStore.setState({ entries: priorOnDisk, queue: [], isWorking: false });
      __fs.failNext('write', new Error('disk full'));

      usePinnedStore.getState().unpin(asTrackId('t1'));

      expect(usePinnedStore.getState().entries['t1']).toBeUndefined();
      expect(__fs.readFile(INDEX_URI)).toBe(JSON.stringify(priorOnDisk));
    });

    it('a read failure makes loadIndex fall back to an empty index instead of crashing app launch', () => {
      __fs.seedFile(INDEX_URI, JSON.stringify({ t1: readyEntry('t1') }));
      __fs.failNext('read', new Error('permission denied'));

      expect(importFreshEntries()).toEqual({});
    });

    it('a createDirectory failure against the index directory makes loadIndex fall back to an empty index', () => {
      __fs.failNext('createDirectory', new Error('disk full'));

      expect(() => importFreshEntries()).not.toThrow();
      expect(importFreshEntries()).toEqual({});
    });

    it('a createDirectory failure against the index directory makes saveIndex silently swallow the write', () => {
      __fs.seedDirectory(AUDIO_DIR_URI);
      usePinnedStore.setState({ entries: { t1: readyEntry('t1') }, queue: [], isWorking: false });
      __fs.failNext('createDirectory', new Error('disk full'));

      usePinnedStore.getState().unpin(asTrackId('t1'));

      expect(usePinnedStore.getState().entries['t1']).toBeUndefined();
      expect(__fs.readFile(INDEX_URI)).toBeUndefined();
    });
  });

  describe('an injected FileStore scopes the pinned index and owner marker to it', () => {
    let store: MemoryFileStore;

    beforeEach(() => {
      store = createMemoryFileStore();
      setPinnedIndexFileStore(store);
    });

    afterEach(() => {
      setPinnedIndexFileStore();
    });

    it('saves, loads and records the owner in the injected store, leaving the device mock untouched', () => {
      saveIndex({ t1: readyEntry('t1') });
      writeOwner('user-a');

      expect([...store.files.keys()].sort()).toEqual([
        'memory://document/offline/pinned-owner',
        'memory://document/offline/pinned.json',
      ]);
      expect(__fs.allFiles()).toEqual({});
      expect(loadIndex()).toEqual({ t1: readyEntry('t1') });
      expect(readOwner()).toBe('user-a');
    });

    it('an index seeded only in the device mock is invisible to a module bound to the injected store', () => {
      __fs.seedFile(INDEX_URI, JSON.stringify({ t1: readyEntry('t1') }));

      expect(loadIndex()).toEqual({});
      expect(readOwner()).toBeNull();
    });
  });

  describe('scheduled index writes coalesce', () => {
    let store: MemoryFileStore;

    beforeEach(() => {
      jest.useFakeTimers();
      store = createMemoryFileStore();
      setPinnedIndexFileStore(store);
    });

    afterEach(() => {
      jest.useRealTimers();
      setPinnedIndexFileStore();
    });

    it('writes only the latest scheduled index, once, when the delay elapses', () => {
      scheduleSaveIndex({ t1: readyEntry('t1') });
      scheduleSaveIndex({ t1: readyEntry('t1'), t2: readyEntry('t2') });

      jest.advanceTimersByTime(INDEX_WRITE_DELAY_MS - 1);
      expect(loadIndex()).toEqual({});

      jest.advanceTimersByTime(1);
      expect(loadIndex()).toEqual({ t1: readyEntry('t1'), t2: readyEntry('t2') });
    });

    it('flushIndex writes a scheduled index now, and is a no-op with nothing scheduled', () => {
      flushIndex();
      expect(store.files.size).toBe(0);

      scheduleSaveIndex({ t1: readyEntry('t1') });
      flushIndex();

      expect(loadIndex()).toEqual({ t1: readyEntry('t1') });
    });

    it('an immediate save supersedes an older scheduled one, which then never lands', () => {
      scheduleSaveIndex({ t1: readyEntry('t1') });
      saveIndex({});

      jest.advanceTimersByTime(INDEX_WRITE_DELAY_MS);

      expect(loadIndex()).toEqual({});
    });
  });
});

describe('reconcile squares the index with the files on disk', () => {
  type FsFailureKind = 'write' | 'read' | 'delete' | 'download' | 'createDirectory';

  const { __fs } = FileSystem as unknown as {
    __fs: {
      reset(): void;
      seedDirectory(uri: string): void;
      seedFile(uri: string, contents: string): void;
      readFile(uri: string): string | undefined;
      allFiles(): Record<string, string>;
      failNext(kind: FsFailureKind, error?: Error): void;
    };
  };

  const AUDIO_DIR = 'file:///document/offline-audio';
  const INDEX_URI = 'file:///document/offline/pinned.json';

  function audioUri(trackId: string): string {
    return `${AUDIO_DIR}/${trackId}.mp3`;
  }

  function resetStore(
    overrides: Partial<{
      entries: Record<string, PinnedEntry>;
      queue: TrackId[];
      isWorking: boolean;
    }> = {},
  ): void {
    usePinnedStore.setState({ entries: {}, queue: [], isWorking: false, ...overrides });
  }

  type PendingCall = {
    ids: readonly string[];
    resolve: (urls: ResolvedAudioUrl[]) => void;
    reject: (err: unknown) => void;
  };

  function captureAudioUrlCalls(): PendingCall[] {
    const calls: PendingCall[] = [];
    fetchAudioUrlsMock.mockImplementation((ids: string[]) => {
      let resolve!: PendingCall['resolve'];
      let reject!: PendingCall['reject'];
      const promise = new Promise<ResolvedAudioUrl[]>((res, rej) => {
        resolve = res;
        reject = rej;
      });
      calls.push({ ids, resolve, reject });
      return promise;
    });
    return calls;
  }

  async function flush(rounds = 20): Promise<void> {
    for (let i = 0; i < rounds; i += 1) {
      await Promise.resolve();
    }
  }

  beforeEach(() => {
    resetStore();
    fetchAudioUrlsMock.mockReset();
  });

  const MATRIX: [PinnedStatus, boolean, 'ready' | 'queued' | 'dropped'][] = [
    ['queued', true, 'ready'],
    ['queued', false, 'queued'],
    ['downloading', true, 'ready'],
    ['downloading', false, 'queued'],
    ['ready', true, 'ready'],
    ['ready', false, 'dropped'],
    ['failed', true, 'ready'],
    ['failed', false, 'dropped'],
  ];

  // A ready cell names its file whether or not the file is still there — that is the stale-ready
  // state reconcile exists to settle; the other statuses name none.
  function entryOfStatus(trackId: string, status: PinnedStatus): PinnedEntry {
    const id = asTrackId(trackId);
    if (status === 'ready') return { trackId: id, status, uri: audioUri(trackId) };
    if (status === 'failed') return { trackId: id, status };
    return { trackId: id, status };
  }

  function seedCell(trackId: string, status: PinnedStatus, filePresent: boolean): void {
    if (filePresent) __fs.seedFile(audioUri(trackId), 'audio-bytes');
    resetStore({ entries: { [trackId]: entryOfStatus(trackId, status) }, isWorking: true });
  }

  describe('reconcile — state x disk matrix', () => {
    it.each(MATRIX)(
      'a %s entry with file present=%s resolves to %s (worker held idle so the classification is observed cleanly)',
      (status, filePresent, expected) => {
        const trackId = `cell-${status}-${String(filePresent)}`;
        seedCell(trackId, status, filePresent);

        usePinnedStore.getState().reconcile();

        const entry = usePinnedStore.getState().entries[trackId];
        if (expected === 'dropped') {
          expect(entry).toBeUndefined();
        } else if (expected === 'ready') {
          expect(entry).toEqual({ trackId, status: 'ready', uri: audioUri(trackId) });
        } else {
          expect(entry).toEqual({ trackId, status: 'queued' });
        }
      },
    );
  });

  describe('reconcile — a mixed index resolves every entry independently', () => {
    it('gets every entry right at once, and the requeue set is exactly the queued/downloading entries whose files are gone', () => {
      __fs.seedFile(audioUri('present-ready'), 'audio-bytes');
      __fs.seedFile(audioUri('present-queued'), 'audio-bytes');
      resetStore({
        entries: {
          'present-ready': {
            trackId: asTrackId('present-ready'),
            status: 'ready',
            uri: 'stale-uri',
          },
          'present-queued': { trackId: asTrackId('present-queued'), status: 'queued' },
          'gone-queued': { trackId: asTrackId('gone-queued'), status: 'queued' },
          'gone-downloading': { trackId: asTrackId('gone-downloading'), status: 'downloading' },
          'gone-ready': { trackId: asTrackId('gone-ready'), status: 'ready', uri: 'stale-uri-2' },
          'gone-failed': { trackId: asTrackId('gone-failed'), status: 'failed' },
        },
        isWorking: true,
      });

      usePinnedStore.getState().reconcile();

      const { entries, queue } = usePinnedStore.getState();
      expect(entries['present-ready']).toEqual({
        trackId: asTrackId('present-ready'),
        status: 'ready',
        uri: audioUri('present-ready'),
      });
      expect(entries['present-queued']).toEqual({
        trackId: asTrackId('present-queued'),
        status: 'ready',
        uri: audioUri('present-queued'),
      });
      expect(entries['gone-queued']).toEqual({
        trackId: asTrackId('gone-queued'),
        status: 'queued',
      });
      expect(entries['gone-downloading']).toEqual({
        trackId: asTrackId('gone-downloading'),
        status: 'queued',
      });
      expect(entries['gone-ready']).toBeUndefined();
      expect(entries['gone-failed']).toBeUndefined();
      expect(Object.keys(entries).sort()).toEqual(
        ['present-ready', 'present-queued', 'gone-queued', 'gone-downloading'].sort(),
      );
      expect([...queue].sort()).toEqual(['gone-downloading', 'gone-queued'].sort());
    });
  });

  describe('reconcile — a file on disk with no index entry', () => {
    it('leaves an orphan file alone: it is not adopted into the index', () => {
      __fs.seedFile(audioUri('orphan-file'), 'audio-bytes');
      resetStore({ entries: {}, isWorking: true });

      usePinnedStore.getState().reconcile();

      expect(usePinnedStore.getState().entries).toEqual({});
    });
  });

  describe('reconcile — idempotence: apply(apply(e)) equals apply(e)', () => {
    it.each(MATRIX)(
      'a %s entry with file present=%s is unchanged by a second reconcile',
      (status, filePresent) => {
        const trackId = `idem-${status}-${String(filePresent)}`;
        seedCell(trackId, status, filePresent);

        usePinnedStore.getState().reconcile();
        const once = usePinnedStore.getState().entries;
        const onceQueue = usePinnedStore.getState().queue;

        usePinnedStore.getState().reconcile();

        expect(usePinnedStore.getState().entries).toEqual(once);
        expect(usePinnedStore.getState().queue).toEqual(onceQueue);
      },
    );

    it('is idempotent across a whole mixed index at once, in memory and on disk', () => {
      __fs.seedFile(audioUri('present-ready'), 'audio-bytes');
      resetStore({
        entries: {
          'present-ready': { trackId: asTrackId('present-ready'), status: 'downloading' },
          'gone-queued': { trackId: asTrackId('gone-queued'), status: 'queued' },
          'gone-ready': { trackId: asTrackId('gone-ready'), status: 'ready', uri: 'stale' },
        },
        isWorking: true,
      });

      usePinnedStore.getState().reconcile();
      const once = usePinnedStore.getState().entries;
      const onceRaw = __fs.readFile(INDEX_URI);

      usePinnedStore.getState().reconcile();

      expect(usePinnedStore.getState().entries).toEqual(once);
      expect(__fs.readFile(INDEX_URI)).toBe(onceRaw);
    });
  });

  describe('reconcile — persistence', () => {
    it('overwrites a stale on-disk index with the freshly rebuilt one, proving reconcile itself writes rather than reading back a stale file', () => {
      __fs.seedFile(
        INDEX_URI,
        JSON.stringify({
          'on-disk': { trackId: asTrackId('on-disk'), status: 'downloading' },
          vanished: { trackId: asTrackId('vanished'), status: 'ready', uri: 'old-uri' },
        }),
      );
      __fs.seedFile(audioUri('on-disk'), 'audio-bytes');
      resetStore({
        entries: {
          'on-disk': { trackId: asTrackId('on-disk'), status: 'queued' },
          vanished: { trackId: asTrackId('vanished'), status: 'ready', uri: 'old-uri' },
        },
        isWorking: true,
      });

      usePinnedStore.getState().reconcile();

      const raw = __fs.readFile(INDEX_URI);
      expect(raw).toBeDefined();
      expect(JSON.parse(raw as string)).toEqual({
        schemaVersion: 1,
        entries: {
          'on-disk': { trackId: asTrackId('on-disk'), status: 'ready', uri: audioUri('on-disk') },
        },
      });
    });

    it('a fresh launch reads the stale persisted index, and reconcile then corrects it and overwrites the stale file', () => {
      let freshFsModule: { __fs: typeof __fs } | undefined;
      let freshStoreModule: { usePinnedStore: typeof usePinnedStore } | undefined;

      jest.isolateModules(() => {
        freshFsModule = require('expo-file-system') as { __fs: typeof __fs };
        freshFsModule.__fs.seedFile(
          INDEX_URI,
          JSON.stringify({
            relaunched: {
              trackId: asTrackId('relaunched'),
              status: 'ready',
              uri: audioUri('relaunched'),
            },
          }),
        );
        freshStoreModule = require('../pinnedStore') as { usePinnedStore: typeof usePinnedStore };
      });

      const freshFs = freshFsModule!.__fs;
      const freshStore = freshStoreModule!.usePinnedStore;

      expect(freshStore.getState().entries['relaunched']).toEqual({
        trackId: asTrackId('relaunched'),
        status: 'ready',
        uri: audioUri('relaunched'),
      });

      freshStore.setState({ isWorking: true });
      freshStore.getState().reconcile();

      expect(freshStore.getState().entries['relaunched']).toBeUndefined();
      expect(JSON.parse(freshFs.readFile(INDEX_URI) as string)).toEqual({
        schemaVersion: 1,
        entries: {},
      });
    });
  });

  describe('reconcile — malformed index entries', () => {
    it('builds the trackId from the index key, not from a missing entry.trackId field', () => {
      __fs.seedFile(audioUri('clean-key'), 'audio-bytes');
      const malformed = { status: 'ready' } as unknown as PinnedEntry;
      resetStore({ entries: { 'clean-key': malformed }, isWorking: true });

      usePinnedStore.getState().reconcile();

      expect(usePinnedStore.getState().entries['clean-key']).toEqual({
        trackId: asTrackId('clean-key'),
        status: 'ready',
        uri: audioUri('clean-key'),
      });
    });

    it('self-heals a trackId field that disagrees with its own index key, since the key is the source of truth', () => {
      __fs.seedFile(audioUri('right-key'), 'audio-bytes');
      resetStore({
        entries: {
          'right-key': { trackId: asTrackId('someone-elses-id'), status: 'ready', uri: 'old' },
        },
        isWorking: true,
      });

      usePinnedStore.getState().reconcile();

      expect(usePinnedStore.getState().entries['right-key']).toEqual({
        trackId: asTrackId('right-key'),
        status: 'ready',
        uri: audioUri('right-key'),
      });
      expect(usePinnedStore.getState().entries['someone-elses-id']).toBeUndefined();
    });

    it('drops an entry carrying a status this version does not recognize when its file is gone, instead of trusting it', () => {
      const malformed = {
        trackId: asTrackId('weird-status'),
        status: 'paused',
      } as unknown as PinnedEntry;
      resetStore({ entries: { 'weird-status': malformed }, isWorking: true });

      usePinnedStore.getState().reconcile();

      expect(usePinnedStore.getState().entries['weird-status']).toBeUndefined();
    });

    it('still trusts the file over an unrecognized status when the file really is there', () => {
      __fs.seedFile(audioUri('weird-status-2'), 'audio-bytes');
      const malformed = {
        trackId: asTrackId('weird-status-2'),
        status: 'paused',
      } as unknown as PinnedEntry;
      resetStore({ entries: { 'weird-status-2': malformed }, isWorking: true });

      usePinnedStore.getState().reconcile();

      expect(usePinnedStore.getState().entries['weird-status-2']).toEqual({
        trackId: asTrackId('weird-status-2'),
        status: 'ready',
        uri: audioUri('weird-status-2'),
      });
    });

    it('does not crash and does not fabricate a ready entry for an empty-string index key', () => {
      resetStore({
        entries: { '': { trackId: '' as TrackId, status: 'queued' } },
        isWorking: true,
      });

      expect(() => usePinnedStore.getState().reconcile()).not.toThrow();
      expect(usePinnedStore.getState().entries['']?.status).not.toBe('ready');
    });
  });

  describe('reconcile — hostile disk contents', () => {
    it('resolves real entries correctly amid a foreign file and a subdirectory in the audio folder', () => {
      __fs.seedFile(`${AUDIO_DIR}/.DS_Store`, 'noise');
      __fs.seedFile(`${AUDIO_DIR}/sub/leftover.mp3`, 'noise');
      __fs.seedFile(audioUri('real-track'), 'audio-bytes');
      resetStore({
        entries: {
          'real-track': { trackId: asTrackId('real-track'), status: 'queued' },
          'missing-track': { trackId: asTrackId('missing-track'), status: 'queued' },
        },
        isWorking: true,
      });

      expect(() => usePinnedStore.getState().reconcile()).not.toThrow();

      const { entries } = usePinnedStore.getState();
      expect(entries['real-track']).toEqual({
        trackId: asTrackId('real-track'),
        status: 'ready',
        uri: audioUri('real-track'),
      });
      expect(entries['missing-track']).toEqual({
        trackId: asTrackId('missing-track'),
        status: 'queued',
      });
    });

    it('does not match a file belonging to a different track whose id merely shares a prefix', () => {
      __fs.seedFile(audioUri('t10'), 'audio-bytes');
      resetStore({
        entries: { t1: { trackId: asTrackId('t1'), status: 'queued' } },
        isWorking: true,
      });

      usePinnedStore.getState().reconcile();

      expect(usePinnedStore.getState().entries['t1']).toEqual({
        trackId: asTrackId('t1'),
        status: 'queued',
      });
    });

    it('requeues a downloading entry whose only file is a leftover partial write instead of adopting it as ready', () => {
      __fs.seedFile(`${AUDIO_DIR}/partial-track.mp3.part`, 'only-a-few-bytes');
      resetStore({
        entries: {
          'partial-track': { trackId: asTrackId('partial-track'), status: 'downloading' },
        },
        isWorking: true,
      });

      usePinnedStore.getState().reconcile();

      expect(usePinnedStore.getState().entries['partial-track']).toEqual({
        trackId: asTrackId('partial-track'),
        status: 'queued',
      });
    });
  });

  describe('reconcile — failure injection', () => {
    it('does not crash the launch when listing the audio directory fails', () => {
      resetStore({
        entries: {
          't-ready': { trackId: asTrackId('t-ready'), status: 'ready', uri: 'stale-uri' },
        },
      });
      __fs.failNext('createDirectory', new Error('EIO: i/o error listing offline-audio'));

      expect(() => usePinnedStore.getState().reconcile()).not.toThrow();
    });

    it('leaves a ready entry untouched when listing the audio directory fails, instead of concluding every file is gone', () => {
      resetStore({
        entries: {
          't-ready': { trackId: asTrackId('t-ready'), status: 'ready', uri: 'stale-uri' },
        },
      });
      __fs.failNext('createDirectory', new Error('EIO: i/o error listing offline-audio'));

      usePinnedStore.getState().reconcile();

      expect(usePinnedStore.getState().entries['t-ready']).toEqual({
        trackId: asTrackId('t-ready'),
        status: 'ready',
        uri: 'stale-uri',
      });
    });

    it('does not persist an emptied index when listing the audio directory fails', () => {
      __fs.seedFile(
        INDEX_URI,
        JSON.stringify({ 't-ready': { trackId: asTrackId('t-ready'), status: 'ready' } }),
      );
      resetStore({
        entries: {
          't-ready': { trackId: asTrackId('t-ready'), status: 'ready', uri: 'stale-uri' },
        },
      });
      __fs.failNext('createDirectory', new Error('EIO: i/o error listing offline-audio'));

      usePinnedStore.getState().reconcile();

      expect(__fs.readFile(INDEX_URI)).toBe(
        JSON.stringify({ 't-ready': { trackId: asTrackId('t-ready'), status: 'ready' } }),
      );
    });

    it('keeps the in-memory index correct for this session even when persisting the rebuilt index to disk fails', () => {
      __fs.seedFile(audioUri('t-ready'), 'audio-bytes');
      resetStore({ entries: { 't-ready': { trackId: asTrackId('t-ready'), status: 'queued' } } });
      __fs.failNext('write', new Error('ENOSPC: no space left on device'));

      expect(() => usePinnedStore.getState().reconcile()).not.toThrow();

      expect(usePinnedStore.getState().entries['t-ready']).toEqual({
        trackId: asTrackId('t-ready'),
        status: 'ready',
        uri: audioUri('t-ready'),
      });
    });
  });

  describe('reconcile — kicks the download worker for anything it requeues', () => {
    it('starts a fresh url resolution for a track that was mid-download when the app died', () => {
      const calls = captureAudioUrlCalls();
      resetStore({
        entries: { interrupted: { trackId: asTrackId('interrupted'), status: 'downloading' } },
      });

      usePinnedStore.getState().reconcile();

      expect(calls).toHaveLength(1);
      expect(calls[0]?.ids).toEqual(['interrupted']);
    });

    it('the kicked retry settles the entry instead of leaving it wedged forever', async () => {
      const calls = captureAudioUrlCalls();
      resetStore({
        entries: { interrupted: { trackId: asTrackId('interrupted'), status: 'downloading' } },
      });

      usePinnedStore.getState().reconcile();

      await act(async () => {
        calls[0]?.resolve([]);
        await flush();
      });

      expect(usePinnedStore.getState().entries['interrupted']?.status).toBe('failed');
      expect(usePinnedStore.getState().queue).toEqual([]);
      expect(usePinnedStore.getState().isWorking).toBe(false);
    });

    it('never kicks the worker when reconcile requeues nothing', () => {
      __fs.seedFile(audioUri('already-ready'), 'audio-bytes');
      const calls = captureAudioUrlCalls();
      resetStore({
        entries: {
          'already-ready': { trackId: asTrackId('already-ready'), status: 'ready', uri: 'stale' },
        },
      });

      usePinnedStore.getState().reconcile();

      expect(calls).toHaveLength(0);
    });
  });

  describe('reconcile — product promises', () => {
    it('does not offer a Track as available offline once its downloaded file has vanished from disk', () => {
      resetStore({
        entries: {
          'vanished-track': {
            trackId: asTrackId('vanished-track'),
            status: 'ready',
            uri: audioUri('vanished-track'),
          },
        },
        isWorking: true,
      });

      usePinnedStore.getState().reconcile();

      expect(resolvePinnedUri(asTrackId('vanished-track'))).toBeUndefined();
    });

    it('retries a Track interrupted mid-download on the next launch instead of forgetting it', () => {
      resetStore({
        entries: { interrupted: { trackId: asTrackId('interrupted'), status: 'downloading' } },
        isWorking: true,
      });

      usePinnedStore.getState().reconcile();

      expect(usePinnedStore.getState().entries['interrupted']).toEqual({
        trackId: asTrackId('interrupted'),
        status: 'queued',
      });
    });

    it('makes a Track available offline purely from what is on disk, even when the index never recorded where the file was', () => {
      __fs.seedFile(audioUri('recovered'), 'audio-bytes');
      resetStore({
        entries: { recovered: { trackId: asTrackId('recovered'), status: 'failed' } },
        isWorking: true,
      });

      usePinnedStore.getState().reconcile();

      expect(resolvePinnedUri(asTrackId('recovered'))).toBe(audioUri('recovered'));
    });
  });
});

describe('the download worker', () => {
  type FsFailureKind = 'write' | 'read' | 'delete' | 'download' | 'createDirectory';

  const { __fs } = FileSystem as unknown as {
    __fs: {
      reset(): void;
      seedDirectory(uri: string): void;
      seedFile(uri: string, contents: string): void;
      readFile(uri: string): string | undefined;
      allFiles(): Record<string, string>;
      failNext(kind: FsFailureKind, error?: Error): void;
    };
  };

  const PINNED_DIR_URI = 'file:///document/offline-audio';
  const INDEX_URI = 'file:///document/offline/pinned.json';

  function pinnedUri(name: string): string {
    return `${PINNED_DIR_URI}/${name}`;
  }

  // Only a ready entry carries a uri, so every read of one narrows on status first.
  function readyUri(trackId: string): string | undefined {
    const entry = usePinnedStore.getState().entries[trackId];
    return entry?.status === 'ready' ? entry.uri : undefined;
  }

  function signedUrl(trackId: string, generation = 1): string {
    return `https://cdn.example.com/audio/${trackId}.mp3?sig=token-${generation}&exp=999`;
  }

  function resolved(trackId: string, generation = 1): ResolvedAudioUrl {
    return { trackId, url: signedUrl(trackId, generation), version: `v${generation}` };
  }

  type PendingCall = {
    ids: readonly string[];
    resolve: (urls: ResolvedAudioUrl[]) => void;
    reject: (err: unknown) => void;
  };

  function captureAudioUrlCalls(): PendingCall[] {
    const calls: PendingCall[] = [];
    fetchAudioUrlsMock.mockImplementation((ids: string[]) => {
      let resolve!: PendingCall['resolve'];
      let reject!: PendingCall['reject'];
      const promise = new Promise<ResolvedAudioUrl[]>((res, rej) => {
        resolve = res;
        reject = rej;
      });
      calls.push({ ids, resolve, reject });
      return promise;
    });
    return calls;
  }

  async function flush(rounds = 20): Promise<void> {
    for (let i = 0; i < rounds; i += 1) {
      await Promise.resolve();
    }
  }

  beforeEach(() => {
    usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
    fetchAudioUrlsMock.mockReset();
  });

  describe('runQueue — isWorking reentrancy guard', () => {
    it('a second pin() arriving while the first track is still downloading does not issue a second concurrent url resolution', async () => {
      const calls = captureAudioUrlCalls();

      usePinnedStore.getState().pin(asTrackId('A'));
      expect(calls).toHaveLength(1);
      expect(calls[0]?.ids).toEqual(['A']);

      usePinnedStore.getState().pin(asTrackId('B'));
      expect(calls).toHaveLength(1);
      expect(usePinnedStore.getState().entries['B']?.status).toBe('queued');

      await act(async () => {
        calls[0]?.resolve([resolved('A')]);
        await flush();
      });

      expect(calls).toHaveLength(2);
      expect(calls[1]?.ids).toEqual(['B']);
      expect(usePinnedStore.getState().entries['A']?.status).toBe('ready');

      await act(async () => {
        calls[1]?.resolve([resolved('B')]);
        await flush();
      });

      expect(usePinnedStore.getState().entries['B']?.status).toBe('ready');
      expect(usePinnedStore.getState().isWorking).toBe(false);
      expect(usePinnedStore.getState().queue).toEqual([]);
    });

    it('pinMany() arriving mid-download queues its tracks behind the running worker instead of starting concurrent downloads', async () => {
      const calls = captureAudioUrlCalls();

      usePinnedStore.getState().pin(asTrackId('A'));
      expect(calls).toHaveLength(1);

      usePinnedStore.getState().pinMany([asTrackId('B'), asTrackId('C')]);
      expect(calls).toHaveLength(1);

      await act(async () => {
        calls[0]?.resolve([resolved('A')]);
        await flush();
      });
      expect(calls).toHaveLength(2);
      expect(calls[1]?.ids).toEqual(['B']);

      await act(async () => {
        calls[1]?.resolve([resolved('B')]);
        await flush();
      });
      expect(calls).toHaveLength(3);
      expect(calls[2]?.ids).toEqual(['C']);

      await act(async () => {
        calls[2]?.resolve([resolved('C')]);
        await flush();
      });

      expect(usePinnedStore.getState().entries['A']?.status).toBe('ready');
      expect(usePinnedStore.getState().entries['B']?.status).toBe('ready');
      expect(usePinnedStore.getState().entries['C']?.status).toBe('ready');
      expect(usePinnedStore.getState().isWorking).toBe(false);
    });

    it('unpin() of the currently-downloading track removes it immediately, and the worker is not left wedged once the in-flight transfer settles', async () => {
      const calls = captureAudioUrlCalls();

      usePinnedStore.getState().pin(asTrackId('A'));
      expect(usePinnedStore.getState().entries['A']?.status).toBe('downloading');

      usePinnedStore.getState().unpin(asTrackId('A'));
      expect(usePinnedStore.getState().entries['A']).toBeUndefined();

      await act(async () => {
        calls[0]?.resolve([resolved('A')]);
        await flush();
      });

      expect(usePinnedStore.getState().entries['A']).toBeUndefined();
      expect(__fs.allFiles()[pinnedUri('A.mp3')]).toBeUndefined();
      expect(usePinnedStore.getState().isWorking).toBe(false);
    });

    it('unpinAll() mid-download (sign-out) tears down the in-flight track and drops any tracks still waiting behind it, without ever resolving their urls', async () => {
      const calls = captureAudioUrlCalls();

      usePinnedStore.getState().pinMany([asTrackId('A'), asTrackId('B')]);
      expect(calls).toHaveLength(1);
      expect(calls[0]?.ids).toEqual(['A']);

      usePinnedStore.getState().unpinAll();
      expect(usePinnedStore.getState().entries).toEqual({});
      expect(usePinnedStore.getState().queue).toEqual([]);

      await act(async () => {
        calls[0]?.resolve([resolved('A')]);
        await flush();
      });

      expect(calls).toHaveLength(1);
      expect(usePinnedStore.getState().entries).toEqual({});
      expect(__fs.readFile(pinnedUri('A.mp3'))).toBeUndefined();
      expect(__fs.readFile(pinnedUri('B.mp3'))).toBeUndefined();
      expect(usePinnedStore.getState().isWorking).toBe(false);
    });
  });

  describe('runQueue — the worker is never wedged after a terminal outcome', () => {
    it('is not left wedged after a failure: a fresh pin right after a failed download starts a new worker immediately', async () => {
      const calls = captureAudioUrlCalls();

      usePinnedStore.getState().pin(asTrackId('A'));
      await act(async () => {
        calls[0]?.reject(new Error('network drop'));
        await flush();
      });
      expect(usePinnedStore.getState().isWorking).toBe(false);

      usePinnedStore.getState().pin(asTrackId('B'));
      expect(usePinnedStore.getState().entries['B']?.status).toBe('downloading');
      expect(calls).toHaveLength(2);
    });
  });

  describe('downloadOne', () => {
    it('resolves a signed url, writes the file and marks the entry ready with the local uri', async () => {
      const calls = captureAudioUrlCalls();

      usePinnedStore.getState().pin(asTrackId('A'));
      expect(usePinnedStore.getState().entries['A']?.status).toBe('downloading');

      await act(async () => {
        calls[0]?.resolve([resolved('A')]);
        await flush();
      });

      expect(usePinnedStore.getState().entries['A']).toEqual({
        trackId: 'A',
        status: 'ready',
        uri: pinnedUri('A.mp3'),
        version: 'v1',
      });
      expect(usePinnedStore.getState().isWorking).toBe(false);
    });

    it('a resolved-urls response that omits the requested id (an empty array) lands the entry in failed rather than hanging the worker', async () => {
      const calls = captureAudioUrlCalls();

      usePinnedStore.getState().pin(asTrackId('A'));

      await act(async () => {
        calls[0]?.resolve([]);
        await flush();
      });

      expect(usePinnedStore.getState().entries['A']?.status).toBe('failed');
      expect(usePinnedStore.getState().isWorking).toBe(false);
      expect(usePinnedStore.getState().queue).toEqual([]);
    });

    it('a rejected url resolution (network drop) marks the entry failed', async () => {
      const calls = captureAudioUrlCalls();

      usePinnedStore.getState().pin(asTrackId('A'));

      await act(async () => {
        calls[0]?.reject(new Error('network drop'));
        await flush();
      });

      expect(usePinnedStore.getState().entries['A']?.status).toBe('failed');
      expect(usePinnedStore.getState().isWorking).toBe(false);
    });

    it('a stream disconnect on every attempt marks the entry failed after the capped retries instead of leaving it stuck downloading', async () => {
      jest.useFakeTimers();
      try {
        const calls = captureAudioUrlCalls();
        usePinnedStore.getState().pin(asTrackId('A'));

        for (let attempt = 0; attempt < 3; attempt += 1) {
          __fs.failNext('download', new Error('stream disconnected'));
          await act(async () => {
            calls[attempt]?.resolve([resolved('A')]);
            await flush();
          });
          if (attempt < 2) {
            expect(usePinnedStore.getState().entries['A']?.status).toBe('downloading');
            await act(async () => {
              await jest.advanceTimersByTimeAsync(4_000);
              await flush();
            });
          }
        }

        expect(calls).toHaveLength(3);
        expect(usePinnedStore.getState().entries['A']?.status).toBe('failed');
        expect(usePinnedStore.getState().isWorking).toBe(false);
      } finally {
        jest.useRealTimers();
      }
    });

    it('a failed track is offered for retry: pinning it again after a failure starts a fresh download rather than staying pending forever', async () => {
      const calls = captureAudioUrlCalls();

      usePinnedStore.getState().pin(asTrackId('A'));
      await act(async () => {
        calls[0]?.resolve([]);
        await flush();
      });
      expect(usePinnedStore.getState().entries['A']?.status).toBe('failed');

      usePinnedStore.getState().pin(asTrackId('A'));
      expect(calls).toHaveLength(2);

      await act(async () => {
        calls[1]?.resolve([resolved('A')]);
        await flush();
      });

      expect(usePinnedStore.getState().entries['A']?.status).toBe('ready');
    });

    it('an index write failure while marking the entry ready leaves in-memory state correct but the persisted index silently stale', async () => {
      const calls = captureAudioUrlCalls();

      usePinnedStore.getState().pin(asTrackId('A'));
      await act(async () => {
        calls[0]?.resolve([resolved('A')]);
        await flush();
      });

      usePinnedStore.getState().pin(asTrackId('B'));
      const beforeFinalWrite = __fs.readFile(INDEX_URI);
      __fs.failNext('write', new Error('disk full'));

      await act(async () => {
        calls[1]?.resolve([resolved('B')]);
        await flush();
      });

      expect(usePinnedStore.getState().entries['B']).toEqual({
        trackId: 'B',
        status: 'ready',
        uri: pinnedUri('B.mp3'),
        version: 'v1',
      });
      expect(__fs.readFile(INDEX_URI)).toBe(beforeFinalWrite);
    });

    it('cleans up the file left behind by an unpin that landed mid-download, once the download succeeds after the unpin', async () => {
      const calls = captureAudioUrlCalls();

      usePinnedStore.getState().pin(asTrackId('A'));
      usePinnedStore.getState().unpin(asTrackId('A'));

      await act(async () => {
        calls[0]?.resolve([resolved('A')]);
        await flush();
      });

      expect(usePinnedStore.getState().entries['A']).toBeUndefined();
      expect(__fs.readFile(pinnedUri('A.mp3'))).toBeUndefined();
    });

    it('also cleans up a stray file at the track path when the download that was unpinned mid-flight instead fails', async () => {
      const calls = captureAudioUrlCalls();

      usePinnedStore.getState().pin(asTrackId('A'));
      usePinnedStore.getState().unpin(asTrackId('A'));
      __fs.seedFile(pinnedUri('A.mp3'), 'partial-bytes-written-during-the-in-flight-attempt');

      await act(async () => {
        calls[0]?.reject(new Error('network drop mid-transfer'));
        await flush();
      });

      expect(usePinnedStore.getState().entries['A']).toBeUndefined();
      expect(__fs.readFile(pinnedUri('A.mp3'))).toBeUndefined();
    });

    it('skips a queued id whose entry was removed before the worker reached it, forced via direct state mutation since the public pin/unpin surface cannot currently produce it', async () => {
      const calls = captureAudioUrlCalls();

      usePinnedStore.getState().pin(asTrackId('A'));
      expect(calls).toHaveLength(1);
      usePinnedStore.setState((s) => ({ queue: [...s.queue, asTrackId('ghost')] }));

      await act(async () => {
        calls[0]?.resolve([resolved('A')]);
        await flush();
      });

      expect(calls).toHaveLength(1);
      expect(usePinnedStore.getState().entries['ghost']).toBeUndefined();
      expect(usePinnedStore.getState().queue).toEqual([]);
      expect(usePinnedStore.getState().isWorking).toBe(false);
    });
  });

  describe('downloadOne — replaying a pin for an already-ready track', () => {
    it('is a no-op: no new url resolution is issued and the entry is unchanged', async () => {
      const calls = captureAudioUrlCalls();

      usePinnedStore.getState().pin(asTrackId('A'));
      await act(async () => {
        calls[0]?.resolve([resolved('A')]);
        await flush();
      });
      const readyEntry = usePinnedStore.getState().entries['A'];
      expect(readyEntry?.status).toBe('ready');

      usePinnedStore.getState().pin(asTrackId('A'));

      expect(calls).toHaveLength(1);
      expect(usePinnedStore.getState().entries['A']).toEqual(readyEntry);
      expect(usePinnedStore.getState().queue).toEqual([]);
    });
  });

  describe('security — the signed download url never reaches disk', () => {
    it('after a successful download, the persisted index and the in-memory entry hold only the local file uri, never the source url or its query string', async () => {
      const calls = captureAudioUrlCalls();
      const url = signedUrl('A', 1);

      usePinnedStore.getState().pin(asTrackId('A'));
      await act(async () => {
        calls[0]?.resolve([{ trackId: 'A', url, version: 'v1' }]);
        await flush();
      });

      expect(readyUri('A')).toBe(pinnedUri('A.mp3'));
      expect(readyUri('A')).not.toContain('?');
      expect(readyUri('A')).not.toContain('sig=');

      const persisted = __fs.readFile(INDEX_URI);
      expect(persisted).not.toContain(url);
      expect(persisted).not.toContain('token-1');
      expect(JSON.parse(persisted as string)).toEqual({
        schemaVersion: 1,
        entries: { A: { trackId: 'A', status: 'ready', uri: pinnedUri('A.mp3'), version: 'v1' } },
      });
    });
  });
});

describe('repinIfPinned re-downloads a pinned track whose audio changed', () => {
  type FsFailureKind = 'write' | 'read' | 'delete' | 'download' | 'createDirectory';

  const { __fs } = FileSystem as unknown as {
    __fs: {
      reset(): void;
      seedDirectory(uri: string): void;
      seedFile(uri: string, contents: string): void;
      readFile(uri: string): string | undefined;
      allFiles(): Record<string, string>;
      failNext(kind: FsFailureKind, error?: Error): void;
    };
  };

  const PINNED_DIR_URI = 'file:///document/offline-audio';

  function pinnedUri(name: string): string {
    return `${PINNED_DIR_URI}/${name}`;
  }

  function signedUrl(trackId: string, generation = 1): string {
    return `https://cdn.example.com/audio/${trackId}.mp3?sig=token-${generation}&exp=999`;
  }

  function resolved(trackId: string, generation = 1): ResolvedAudioUrl {
    return { trackId, url: signedUrl(trackId, generation), version: `v${generation}` };
  }

  type PendingCall = {
    ids: readonly string[];
    resolve: (urls: ResolvedAudioUrl[]) => void;
    reject: (err: unknown) => void;
  };

  function captureAudioUrlCalls(): PendingCall[] {
    const calls: PendingCall[] = [];
    fetchAudioUrlsMock.mockImplementation((ids: string[]) => {
      let resolve!: PendingCall['resolve'];
      let reject!: PendingCall['reject'];
      const promise = new Promise<ResolvedAudioUrl[]>((res, rej) => {
        resolve = res;
        reject = rej;
      });
      calls.push({ ids, resolve, reject });
      return promise;
    });
    return calls;
  }

  async function flush(rounds = 20): Promise<void> {
    for (let i = 0; i < rounds; i += 1) {
      await Promise.resolve();
    }
  }

  async function drainWorker(calls: PendingCall[], nextIndex: number): Promise<string[]> {
    const usedUrls: string[] = [];
    let i = nextIndex;
    while (usePinnedStore.getState().isWorking) {
      const call = calls[i];
      if (call === undefined) {
        throw new Error(
          'worker still marked isWorking with no pending fetchAudioUrls call left to resolve',
        );
      }
      const urls = call.ids.map((id) => resolved(id, i + 1));
      call.resolve(urls);
      usedUrls.push(urls[0]?.url ?? '');
      i += 1;
      await flush();
    }
    return usedUrls;
  }

  beforeEach(() => {
    usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
    fetchAudioUrlsMock.mockReset();
  });

  describe('repinIfPinned — guard arm', () => {
    it('does nothing for a track that was never pinned: no url resolution is issued and no entry is created', () => {
      const calls = captureAudioUrlCalls();

      repinIfPinned(asTrackId('never-pinned'));

      expect(calls).toHaveLength(0);
      expect(usePinnedStore.getState().entries['never-pinned']).toBeUndefined();
      expect(usePinnedStore.getState().queue).toEqual([]);
    });
  });

  describe('repinIfPinned — replace arm', () => {
    it('re-downloads a previously-downloaded track under the same key, ending with the new audio instead of the old', async () => {
      const calls = captureAudioUrlCalls();

      usePinnedStore.getState().pin(asTrackId('A'));
      await act(async () => {
        calls[0]?.resolve([resolved('A', 1)]);
        await flush();
      });
      expect(usePinnedStore.getState().entries['A']?.status).toBe('ready');
      expect(__fs.readFile(pinnedUri('A.mp3'))).toBe(`downloaded:${signedUrl('A', 1)}`);

      repinIfPinned(asTrackId('A'));
      expect(__fs.readFile(pinnedUri('A.mp3'))).toBeUndefined();
      expect(usePinnedStore.getState().entries['A']?.status).toBe('downloading');
      expect(calls).toHaveLength(2);

      await act(async () => {
        calls[1]?.resolve([resolved('A', 2)]);
        await flush();
      });

      expect(usePinnedStore.getState().entries['A']).toEqual({
        trackId: 'A',
        status: 'ready',
        uri: pinnedUri('A.mp3'),
        version: 'v2',
      });
      expect(__fs.readFile(pinnedUri('A.mp3'))).toBe(`downloaded:${signedUrl('A', 2)}`);
    });

    it('replaying repinIfPinned twice in a row for the same track leaves exactly one entry and one queue slot once the worker settles', async () => {
      const calls = captureAudioUrlCalls();

      usePinnedStore.getState().pin(asTrackId('A'));
      await act(async () => {
        calls[0]?.resolve([resolved('A', 1)]);
        await flush();
      });

      repinIfPinned(asTrackId('A'));
      repinIfPinned(asTrackId('A'));
      expect(calls).toHaveLength(2);
      expect(usePinnedStore.getState().queue).toEqual(['A']);

      const usedUrls = await drainWorker(calls, 1);

      expect(Object.keys(usePinnedStore.getState().entries)).toEqual(['A']);
      expect(usePinnedStore.getState().queue).toEqual([]);
      expect(usePinnedStore.getState().entries['A']?.status).toBe('ready');
      const lastUrl = usedUrls[usedUrls.length - 1];
      expect(__fs.readFile(pinnedUri('A.mp3'))).toBe(`downloaded:${lastUrl}`);
    });
  });

  describe('repinIfPinned firing while the track is currently downloading', () => {
    it('joins the running worker instead of starting a second live download, and the final ready entry holds the NEW audio', async () => {
      const calls = captureAudioUrlCalls();

      usePinnedStore.getState().pin(asTrackId('A'));
      expect(usePinnedStore.getState().entries['A']?.status).toBe('downloading');
      expect(calls).toHaveLength(1);

      repinIfPinned(asTrackId('A'));

      expect(calls).toHaveLength(1);
      expect(usePinnedStore.getState().entries['A']?.status).toBe('queued');
      expect(usePinnedStore.getState().queue).toEqual(['A']);

      await act(async () => {
        calls[0]?.resolve([resolved('A', 1)]);
        await flush();
      });

      expect(calls).toHaveLength(2);
      expect(calls[1]?.ids).toEqual(['A']);
      expect(usePinnedStore.getState().entries['A']?.status).toBe('downloading');

      await act(async () => {
        calls[1]?.resolve([resolved('A', 2)]);
        await flush();
      });

      expect(usePinnedStore.getState().entries['A']).toEqual({
        trackId: 'A',
        status: 'ready',
        uri: pinnedUri('A.mp3'),
        version: 'v2',
      });
      expect(__fs.readFile(pinnedUri('A.mp3'))).toBe(`downloaded:${signedUrl('A', 2)}`);
      expect(usePinnedStore.getState().isWorking).toBe(false);
      expect(usePinnedStore.getState().queue).toEqual([]);
    });

    it('never publishes the superseded download as ready, and never leaves its bytes on disk, even transiently while the fresh download is still queued behind it', async () => {
      const calls = captureAudioUrlCalls();

      usePinnedStore.getState().pin(asTrackId('A'));
      repinIfPinned(asTrackId('A'));
      expect(usePinnedStore.getState().entries['A']).toEqual({ trackId: 'A', status: 'queued' });

      let sawStaleReady = false;
      await act(async () => {
        calls[0]?.resolve([resolved('A', 1)]);
        for (let i = 0; i < 60; i += 1) {
          await Promise.resolve();
          const entry = usePinnedStore.getState().entries['A'];
          if (entry?.status === 'ready' && entry.uri === pinnedUri('A.mp3')) sawStaleReady = true;
        }
      });

      expect(sawStaleReady).toBe(false);
      expect(__fs.readFile(pinnedUri('A.mp3'))).toBeUndefined();
    });

    it('performs no status write at all for the superseded download: the entry stays exactly what the re-pin left it as until the fresh download reaches its own worker turn', async () => {
      const calls = captureAudioUrlCalls();

      usePinnedStore.getState().pin(asTrackId('A'));
      repinIfPinned(asTrackId('A'));
      const afterRepin = usePinnedStore.getState().entries['A'];

      let sawUnexpectedWrite = false;
      await act(async () => {
        calls[0]?.resolve([resolved('A', 1)]);
        for (let i = 0; i < 60; i += 1) {
          await Promise.resolve();
          const status = usePinnedStore.getState().entries['A']?.status;
          if (status !== undefined && status !== afterRepin?.status && status !== 'downloading') {
            sawUnexpectedWrite = true;
          }
        }
      });

      expect(sawUnexpectedWrite).toBe(false);
    });
  });
});

describe('pinned audio is served only at the version the server currently serves', () => {
  const { __fs } = FileSystem as unknown as {
    __fs: { readFile(uri: string): string | undefined };
  };

  const PINNED_A = 'file:///document/offline-audio/A.mp3';

  function readyEntry(version?: string) {
    return {
      A: {
        trackId: asTrackId('A'),
        status: 'ready' as const,
        uri: PINNED_A,
        ...(version === undefined ? {} : { version }),
      },
    };
  }

  async function flush(rounds = 20): Promise<void> {
    for (let i = 0; i < rounds; i += 1) {
      await Promise.resolve();
    }
  }

  beforeEach(() => {
    usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
    fetchAudioUrlsMock.mockReset();
    fetchAudioUrlsMock.mockResolvedValue([]);
  });

  describe('resolvePinnedUri — version gate', () => {
    it('returns the local copy when the pinned version is the one the server currently serves', () => {
      usePinnedStore.setState({ entries: readyEntry('v1') });

      expect(resolvePinnedUri(asTrackId('A'), 'v1')).toBe(PINNED_A);
    });

    it('REGRESSION: refuses the local copy when the server has since re-acquired the track under a new version', () => {
      usePinnedStore.setState({ entries: readyEntry('v1') });

      expect(resolvePinnedUri(asTrackId('A'), 'v2')).toBeUndefined();
    });

    it('REGRESSION: refuses a local copy pinned before versions existed once the server reports a version', () => {
      usePinnedStore.setState({ entries: readyEntry(undefined) });

      expect(resolvePinnedUri(asTrackId('A'), 'v2')).toBeUndefined();
    });

    it('serves the local copy when the caller has no version to check against — an offline load must still play', () => {
      usePinnedStore.setState({ entries: readyEntry('v1') });

      expect(resolvePinnedUri(asTrackId('A'), undefined)).toBe(PINNED_A);
    });

    it('serves the local copy when the server itself reports no version, so a never-re-acquired track is not re-downloaded on every play', () => {
      usePinnedStore.setState({ entries: readyEntry('v1') });

      expect(resolvePinnedUri(asTrackId('A'), '')).toBe(PINNED_A);
    });

    it('stays gated on status: a version match does not resurrect a failed entry', () => {
      usePinnedStore.setState({
        // #1766 made a failed entry carrying a uri and a version unrepresentable, so the fixture is
        // forced past the type: the status check stays as defence in depth for state planted that way.
        entries: {
          A: { trackId: asTrackId('A'), status: 'failed', uri: PINNED_A, version: 'v1' },
        } as unknown as Record<string, PinnedEntry>,
      });

      expect(resolvePinnedUri(asTrackId('A'), 'v1')).toBeUndefined();
    });
  });

  describe('resolvePinnedUri — self-healing on a version mismatch', () => {
    it('refuses the stale copy and takes it off ready in the same call, so no ordering can serve it', () => {
      usePinnedStore.setState({ entries: readyEntry('v1') });
      fetchAudioUrlsMock.mockReturnValue(new Promise<ResolvedAudioUrl[]>(() => {}));

      expect(resolvePinnedUri(asTrackId('A'), 'v2')).toBeUndefined();

      expect(usePinnedStore.getState().entries['A']?.status).not.toBe('ready');
      expect(fetchAudioUrlsMock).toHaveBeenCalledTimes(1);
    });

    it('REGRESSION: a stale pinned file is replaced with the current audio without any acquisition event arriving', async () => {
      usePinnedStore.setState({ entries: readyEntry('v1') });
      fetchAudioUrlsMock.mockResolvedValue([
        { trackId: 'A', url: 'https://cdn.example/A.mp3?gen=2', version: 'v2' },
      ]);

      resolvePinnedUri(asTrackId('A'), 'v2');

      await act(async () => {
        await flush();
      });

      expect(usePinnedStore.getState().entries['A']).toEqual({
        trackId: 'A',
        status: 'ready',
        uri: PINNED_A,
        version: 'v2',
      });
      expect(__fs.readFile(PINNED_A)).toBe('downloaded:https://cdn.example/A.mp3?gen=2');
      expect(resolvePinnedUri(asTrackId('A'), 'v2')).toBe(PINNED_A);
    });

    it('leaves a matching version in place — no re-pin when the local copy is current', () => {
      usePinnedStore.setState({ entries: readyEntry('v1') });

      expect(resolvePinnedUri(asTrackId('A'), 'v1')).toBe(PINNED_A);

      expect(fetchAudioUrlsMock).not.toHaveBeenCalled();
      expect(usePinnedStore.getState().queue).toEqual([]);
    });

    it('does not re-pin a track that was never pinned', () => {
      expect(resolvePinnedUri(asTrackId('never-pinned'), 'v2')).toBeUndefined();

      expect(fetchAudioUrlsMock).not.toHaveBeenCalled();
      expect(usePinnedStore.getState().entries['never-pinned']).toBeUndefined();
    });

    it('does not stack re-pins while the replacement is already in flight', () => {
      usePinnedStore.setState({ entries: readyEntry('v1') });
      const pending = new Promise<ResolvedAudioUrl[]>(() => {});
      fetchAudioUrlsMock.mockReturnValue(pending);

      resolvePinnedUri(asTrackId('A'), 'v2');
      resolvePinnedUri(asTrackId('A'), 'v2');
      resolvePinnedUri(asTrackId('A'), 'v2');

      expect(fetchAudioUrlsMock).toHaveBeenCalledTimes(1);
      expect(usePinnedStore.getState().queue).toEqual([]);
    });
  });
});

describe('resolvePinnedUri serves only ready local copies', () => {
  beforeEach(() => {
    usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
  });

  describe('resolvePinnedUri — every branch and boundary of the status check', () => {
    it.each<[string, PinnedEntry | undefined, string | undefined]>([
      [
        'ready with a uri — plays with no connectivity',
        { trackId: asTrackId('t1'), status: 'ready', uri: 'file:///document/offline-audio/t1.mp3' },
        'file:///document/offline-audio/t1.mp3',
      ],
      [
        'ready without a uri — degrades to undefined (falls back to network) instead of crashing',
        // #1766 made this state unrepresentable, so it can only be forced past the type. The
        // status check stays as defence in depth for state a cast or an older build could plant.
        { trackId: asTrackId('t1'), status: 'ready' } as unknown as PinnedEntry,
        undefined,
      ],
      ['queued', { trackId: asTrackId('t1'), status: 'queued' }, undefined],
      ['downloading', { trackId: asTrackId('t1'), status: 'downloading' }, undefined],
      ['failed', { trackId: asTrackId('t1'), status: 'failed' }, undefined],
      [
        'failed but carrying a stray uri left over from a prior ready state — status still gates it',
        {
          trackId: asTrackId('t1'),
          status: 'failed',
          uri: 'file:///document/offline-audio/t1.mp3',
        } as unknown as PinnedEntry,
        undefined,
      ],
      ['no entry at all for the track', undefined, undefined],
    ])('%s', (_label, entry, expected) => {
      usePinnedStore.setState({ entries: entry ? { t1: entry } : {} });

      expect(resolvePinnedUri(asTrackId('t1'))).toBe(expected);
    });
  });
});

describe('a download cut off by an app kill is not served as ready', () => {
  const AUDIO_DIR = 'memory://document/offline-audio';
  const T1 = asTrackId('t1');

  function resolved(trackId: string): ResolvedAudioUrl {
    return {
      trackId,
      url: `https://cdn.example.com/audio/${trackId}.mp3?sig=secret`,
      version: 'v1',
    };
  }

  function filesInAudioDir(store: MemoryFileStore): string[] {
    return [...store.files.keys()].filter((uri) => uri.startsWith(`${AUDIO_DIR}/`));
  }

  async function flush(rounds = 40): Promise<void> {
    for (let i = 0; i < rounds; i += 1) {
      await Promise.resolve();
    }
  }

  async function killMidDownloadThenRelaunch(store: MemoryFileStore): Promise<void> {
    store.download = (_url, dest) => {
      dest.write('first-few-bytes');
      return new Promise<string>(() => {});
    };
    usePinnedStore.getState().pin(T1);
    await act(async () => flush());
    expect(usePinnedStore.getState().entries['t1']?.status).toBe('downloading');
    fetchAudioUrlsMock.mockImplementation(() => new Promise<ResolvedAudioUrl[]>(() => {}));
    usePinnedStore.setState({ queue: [], isWorking: false });
  }

  let store: MemoryFileStore;

  beforeEach(() => {
    jest.useFakeTimers();
    store = createMemoryFileStore();
    setPinnedFileStore(store);
    usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
    fetchAudioUrlsMock.mockReset();
    fetchAudioUrlsMock.mockImplementation(async ([id]) => [resolved(id!)]);
  });

  afterEach(() => {
    setPinnedFileStore();
    jest.useRealTimers();
  });

  describe('a pinned download cut off by an app kill', () => {
    it('is downloaded again on the next launch instead of being served as a ready offline copy', async () => {
      await killMidDownloadThenRelaunch(store);

      usePinnedStore.getState().reconcile();

      expect(usePinnedStore.getState().entries['t1']).toEqual({
        trackId: T1,
        status: 'downloading',
      });
      expect(resolvePinnedUri(T1)).toBeUndefined();
    });

    it('leaves no partial bytes behind once the next launch has reconciled', async () => {
      await killMidDownloadThenRelaunch(store);

      usePinnedStore.getState().reconcile();

      expect(filesInAudioDir(store)).toEqual([]);
    });
  });

  describe('reconcile with an unfinished download file on disk', () => {
    it.each(['queued', 'downloading'] as const)(
      'does not adopt the unfinished file of a %s entry as ready',
      (status) => {
        store.files.set(`${AUDIO_DIR}/t1.mp3.tmp`, 'first-few-bytes');
        usePinnedStore.setState({ entries: { t1: { trackId: T1, status } }, isWorking: true });

        usePinnedStore.getState().reconcile();

        expect(usePinnedStore.getState().entries['t1']).toEqual({ trackId: T1, status: 'queued' });
      },
    );

    it('keeps the unfinished file while a drain is running, since it may be the transfer in flight', () => {
      store.files.set(`${AUDIO_DIR}/t1.mp3.tmp`, 'first-few-bytes');
      usePinnedStore.setState({
        entries: { t1: { trackId: T1, status: 'downloading' } },
        isWorking: true,
      });

      usePinnedStore.getState().reconcile();

      expect(filesInAudioDir(store)).toEqual([`${AUDIO_DIR}/t1.mp3.tmp`]);
    });

    it('still adopts a finished download whose ready status never reached the index', () => {
      store.files.set(`${AUDIO_DIR}/t1.mp3`, 'whole-track');
      usePinnedStore.setState({
        entries: { t1: { trackId: T1, status: 'downloading' } },
        isWorking: true,
      });

      usePinnedStore.getState().reconcile();

      expect(usePinnedStore.getState().entries['t1']).toEqual({
        trackId: T1,
        status: 'ready',
        uri: `${AUDIO_DIR}/t1.mp3`,
      });
    });
  });
});

describe('pinned download deadlines, failure logging and the storage cap', () => {
  function resolved(trackId: string): ResolvedAudioUrl {
    return {
      trackId,
      url: `https://cdn.example.com/audio/${trackId}.mp3?sig=secret`,
      version: 'v1',
    };
  }

  async function flush(rounds = 40): Promise<void> {
    for (let i = 0; i < rounds; i += 1) {
      await Promise.resolve();
    }
  }

  // Reports every pinned file as `size` bytes, so a cap in the gigabytes can be reached without
  // holding gigabytes of test data.
  function withFileSize(store: MemoryFileStore, size: number): MemoryFileStore {
    const sized = (file: StoredFile): StoredFile => ({
      uri: file.uri,
      get exists() {
        return file.exists;
      },
      get size() {
        return file.exists ? size : null;
      },
      textSync: () => file.textSync(),
      write: (contents) => file.write(contents),
      delete: () => file.delete(),
      moveTo: (dest) => file.moveTo(dest),
    });
    const openDirectory = store.openDirectory;
    store.openDirectory = (name): StoredDirectory => {
      const dir = openDirectory(name);
      return {
        uri: dir.uri,
        get exists() {
          return dir.exists;
        },
        create: () => dir.create(),
        list: () => dir.list().map(sized),
        openFile: (fileName) => sized(dir.openFile(fileName)),
      };
    };
    return store;
  }

  let store: MemoryFileStore;
  let warn: jest.SpyInstance;

  beforeEach(() => {
    store = createMemoryFileStore();
    setPinnedFileStore(store);
    usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
    fetchAudioUrlsMock.mockReset();
    fetchAudioUrlsMock.mockImplementation(async ([id]) => [resolved(id!)]);
    warn = jest.spyOn(console, 'warn').mockImplementation(() => {});
  });

  afterEach(() => {
    jest.useRealTimers();
    setPinnedFileStore();
    warn.mockRestore();
  });

  // A stalled transfer times out as a transient failure, so it is attempted three times: each
  // spends the deadline, and the two backoffs in between wait at most 2s then 4s.
  async function advanceThroughAttempts(): Promise<void> {
    for (let attempt = 0; attempt < 3; attempt += 1) {
      await jest.advanceTimersByTimeAsync(PIN_DOWNLOAD_TIMEOUT_MS);
      await flush();
      await jest.advanceTimersByTimeAsync(4_000);
      await flush();
    }
  }

  describe('a pinned download has its own deadline', () => {
    it('fails a stalled download once the deadline passes and moves the queue on to the next track', async () => {
      jest.useFakeTimers();
      const download = store.download;
      store.download = (url, dest, signal) =>
        url.includes('/stalled.mp3') ? new Promise<string>(() => {}) : download(url, dest, signal);

      const batch = usePinnedStore.getState().pinMany([asTrackId('stalled'), asTrackId('next')]);
      await act(async () => flush());
      expect(usePinnedStore.getState().entries['stalled']?.status).toBe('downloading');
      expect(usePinnedStore.getState().entries['next']?.status).toBe('queued');

      await act(async () => {
        await advanceThroughAttempts();
      });

      expect(usePinnedStore.getState().entries['stalled']?.status).toBe('failed');
      expect(usePinnedStore.getState().entries['next']?.status).toBe('ready');
      expect(usePinnedStore.getState().isWorking).toBe(false);
      await expect(batch).resolves.toEqual({ requested: 2, failed: 1 });
    });

    it('aborts the stalled transfer and leaves no partial file behind to be adopted as ready', async () => {
      jest.useFakeTimers();
      let aborted = false;
      store.download = (_url, dest, signal) => {
        dest.write('partial');
        signal.addEventListener('abort', () => (aborted = true));
        return new Promise<string>(() => {});
      };

      void usePinnedStore.getState().pinMany([asTrackId('stalled')]);
      await act(async () => {
        await flush();
        await advanceThroughAttempts();
      });

      expect(aborted).toBe(true);
      expect(store.files.size).toBe(0);
      expect(usePinnedStore.getState().entries['stalled']?.status).toBe('failed');
    });
  });

  describe('a failed pinned download is logged', () => {
    it('logs the track id, the unsigned url and the caught error', async () => {
      jest.useFakeTimers();
      store.download = () => Promise.reject(new Error('disk full'));

      await act(async () => {
        const batch = usePinnedStore.getState().pinMany([asTrackId('t1')]);
        await flush();
        await jest.advanceTimersByTimeAsync(10_000);
        await batch;
      });

      expect(usePinnedStore.getState().entries['t1']?.status).toBe('failed');
      expect(warn).toHaveBeenCalledWith(expect.stringContaining('t1'), {
        url: 'https://cdn.example.com/audio/t1.mp3',
        error: expect.objectContaining({
          name: 'NetworkError',
          failure: 'transport',
          message: expect.stringContaining('disk full'),
        }),
      });
      expect(JSON.stringify(warn.mock.calls)).not.toContain('secret');
    });

    it('logs a failure to resolve the signed url with no url', async () => {
      const error = new Error('404');
      fetchAudioUrlsMock.mockRejectedValue(error);

      await act(async () => {
        await usePinnedStore.getState().pinMany([asTrackId('t1')]);
      });

      expect(warn).toHaveBeenCalledWith(expect.stringContaining('t1'), { url: undefined, error });
    });
  });

  describe('pinning is refused once pinned storage is full', () => {
    it('pinMany queues nothing and reports the refusal when pinned bytes reach the cap', async () => {
      withFileSize(store, MAX_PINNED_BYTES);
      store.files.set('memory://document/offline-audio/old.mp3', 'x');

      const result = await usePinnedStore.getState().pinMany([asTrackId('a'), asTrackId('b')]);

      expect(result).toEqual({ requested: 0, failed: 0, refused: 'storage-full' });
      expect(usePinnedStore.getState().entries).toEqual({});
      expect(usePinnedStore.getState().queue).toEqual([]);
      expect(fetchAudioUrlsMock).not.toHaveBeenCalled();
    });

    it('pin refuses when free disk space is below the reserve', () => {
      store.freeBytes = MIN_FREE_BYTES - 1;

      expect(usePinnedStore.getState().pin(asTrackId('a'))).toBe('storage-full');
      expect(usePinnedStore.getState().entries).toEqual({});
      expect(fetchAudioUrlsMock).not.toHaveBeenCalled();
    });

    it('pin accepts when there is room, and when the track needs no download', () => {
      expect(usePinnedStore.getState().pin(asTrackId('a'))).toBe('accepted');
      store.freeBytes = 0;
      expect(usePinnedStore.getState().pin(asTrackId('a'))).toBe('accepted');
    });

    it('does not block pinning when free disk space cannot be read', async () => {
      store.availableBytes = () => {
        throw new Error('unsupported');
      };

      await act(async () => {
        await expect(usePinnedStore.getState().pinMany([asTrackId('a')])).resolves.toEqual({
          requested: 1,
          failed: 0,
        });
      });
      expect(usePinnedStore.getState().entries['a']?.status).toBe('ready');
    });

    it('fails a queued track once the batch draining ahead of it has reached the byte cap', async () => {
      withFileSize(store, MAX_PINNED_BYTES / 2);

      await act(async () => {
        await expect(
          usePinnedStore.getState().pinMany([asTrackId('a'), asTrackId('b'), asTrackId('c')]),
        ).resolves.toEqual({ requested: 3, failed: 1 });
      });

      const { entries } = usePinnedStore.getState();
      expect(entries['b']?.status).toBe('ready');
      expect(entries['c']?.status).toBe('failed');
    });

    it('fails a queued track whose turn comes after storage filled up, without downloading it', async () => {
      let download!: () => void;
      fetchAudioUrlsMock.mockImplementationOnce(
        ([id]) =>
          new Promise<ResolvedAudioUrl[]>((res) => {
            download = () => res([resolved(id!)]);
          }),
      );

      const batch = usePinnedStore.getState().pinMany([asTrackId('a'), asTrackId('b')]);
      await act(async () => {
        await flush();
        store.freeBytes = 0;
        download();
        await flush();
      });

      await expect(batch).resolves.toEqual({ requested: 2, failed: 1 });
      expect(usePinnedStore.getState().entries['a']?.status).toBe('ready');
      expect(usePinnedStore.getState().entries['b']?.status).toBe('failed');
      expect(fetchAudioUrlsMock).toHaveBeenCalledTimes(1);
    });
  });
});

describe('unpinMany reports what a bulk removal left downloaded', () => {
  beforeEach(() => {
    fetchAudioUrlsMock.mockReset();
  });

  const AUDIO_DIR = 'memory://document/offline-audio';

  let memory: MemoryFileStore;
  let warn: jest.SpyInstance;

  function audioUri(trackId: string): string {
    return `${AUDIO_DIR}/${trackId}.mp3`;
  }

  function seedDownloads(trackIds: readonly string[]): TrackId[] {
    const entries: Record<string, PinnedEntry> = {};
    for (const trackId of trackIds) {
      memory.files.set(audioUri(trackId), 'audio');
      entries[trackId] = { trackId: asTrackId(trackId), status: 'ready', uri: audioUri(trackId) };
    }
    usePinnedStore.setState({ entries, queue: [], isWorking: false });
    return trackIds.map(asTrackId);
  }

  function refuseDeleteOf(trackId: string): void {
    const deleteFile = memory.files.delete.bind(memory.files);
    memory.files.delete = (uri) => {
      if (uri === audioUri(trackId)) throw new Error('EBUSY: file is locked');
      return deleteFile(uri);
    };
  }

  beforeEach(() => {
    memory = createMemoryFileStore();
    memory.directories.add(AUDIO_DIR);
    setPinnedFileStore(memory);
    setPinnedIndexFileStore(memory);
    usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
    warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    setPinnedFileStore();
    setPinnedIndexFileStore();
    jest.restoreAllMocks();
  });

  describe('unpinMany — a bulk removal reports what is still downloaded', () => {
    it('removes every download it was given and reports nothing left behind', async () => {
      const trackIds = seedDownloads(['d1', 'd2', 'd3']);

      const result = await usePinnedStore.getState().unpinMany(trackIds);

      expect(result).toEqual({ requested: 3, failed: 0 });
      expect(usePinnedStore.getState().entries).toEqual({});
      expect([...memory.files.keys()].filter((uri) => uri.startsWith(AUDIO_DIR))).toEqual([]);
    });

    it('keeps a download whose file the OS would not release, and reports it as not removed', async () => {
      const trackIds = seedDownloads(['d1', 'd2', 'd3']);
      refuseDeleteOf('d2');

      const result = await usePinnedStore.getState().unpinMany(trackIds);

      expect(result).toEqual({ requested: 3, failed: 1 });
      expect(usePinnedStore.getState().entries).toEqual({
        d2: { trackId: 'd2', status: 'ready', uri: audioUri('d2') },
      });
      expect(warn).toHaveBeenCalledWith(
        expect.stringContaining('1 of 3 download(s) could not be removed'),
      );
    });

    it('counts a track named twice in the selection once', async () => {
      seedDownloads(['d1']);

      const result = await usePinnedStore.getState().unpinMany([asTrackId('d1'), asTrackId('d1')]);

      expect(result).toEqual({ requested: 1, failed: 0 });
    });

    it('cancels a queued download while keeping a ready one the filesystem will not let go', async () => {
      seedDownloads(['ready1']);
      usePinnedStore.setState({
        entries: {
          ...usePinnedStore.getState().entries,
          queued1: { trackId: asTrackId('queued1'), status: 'queued' },
        },
        queue: [asTrackId('queued1')],
      });
      refuseDeleteOf('ready1');

      const result = await usePinnedStore
        .getState()
        .unpinMany([asTrackId('ready1'), asTrackId('queued1')]);

      expect(result).toEqual({ requested: 2, failed: 1 });
      expect(Object.keys(usePinnedStore.getState().entries)).toEqual(['ready1']);
      expect(usePinnedStore.getState().queue).toEqual([]);
    });

    it('reports nothing left behind when the same selection is removed twice at once', async () => {
      const trackIds = seedDownloads(['d1', 'd2', 'd3']);
      const { unpinMany } = usePinnedStore.getState();

      const [first, second] = await Promise.all([unpinMany(trackIds), unpinMany(trackIds)]);

      expect(first).toEqual({ requested: 3, failed: 0 });
      expect(second).toEqual({ requested: 3, failed: 0 });
      expect(usePinnedStore.getState().entries).toEqual({});
    });

    it('keeps every ready download when the pinned directory cannot be listed at all', async () => {
      const trackIds = seedDownloads(['d1', 'd2']);
      const openDirectory = memory.openDirectory;
      memory.openDirectory = (name) => ({
        ...openDirectory(name),
        list: () => {
          throw new Error('EIO: listing failed');
        },
      });

      const result = await usePinnedStore.getState().unpinMany(trackIds);

      expect(result).toEqual({ requested: 2, failed: 2 });
      expect(Object.keys(usePinnedStore.getState().entries)).toEqual(['d1', 'd2']);
    });

    it('stops starting removals once the batch runs past its deadline, reporting the rest as left', async () => {
      const trackIds = seedDownloads(Array.from({ length: 400 }, (_, i) => `d${i}`));
      let elapsedMs = 0;
      jest.spyOn(performance, 'now').mockImplementation(() => elapsedMs);
      const deleteFile = memory.files.delete.bind(memory.files);
      // A filesystem taking a second per delete: the batch cannot finish inside its deadline.
      memory.files.delete = (uri) => {
        elapsedMs += 1_000;
        return deleteFile(uri);
      };

      const result = await usePinnedStore.getState().unpinMany(trackIds);

      expect(result.requested).toBe(400);
      expect(result.failed).toBeGreaterThan(0);
      expect(result.failed).toBeLessThan(400);
      expect(Object.keys(usePinnedStore.getState().entries)).toHaveLength(result.failed);
    });
  });
});

describe('work stays bounded on a large pinned library', () => {
  // A launch-sized library: large enough that per-entry directory listings or per-transition
  // index writes show up as thousands of operations rather than a handful.
  const LIBRARY_SIZE = 1200;

  type Counts = { audioLists: number; indexWrites: number };

  // Counts the filesystem operations that dominate launch and batch-pin cost: listing the pinned
  // audio directory and rewriting the whole pinned index.
  function counting(store: MemoryFileStore): { store: MemoryFileStore; counts: Counts } {
    const counts: Counts = { audioLists: 0, indexWrites: 0 };
    const openDirectory = store.openDirectory;
    const countedFile = (file: StoredFile): StoredFile => ({
      uri: file.uri,
      get exists() {
        return file.exists;
      },
      get size() {
        return file.size;
      },
      textSync: () => file.textSync(),
      write: (contents) => file.write(contents),
      delete: () => file.delete(),
      // The index is written to a temp file and renamed into place: each rename is one rewrite.
      moveTo: (dest) => {
        if (dest.uri.endsWith('/pinned.json')) counts.indexWrites += 1;
        file.moveTo(dest);
      },
    });
    store.openDirectory = (name): StoredDirectory => {
      const dir = openDirectory(name);
      return {
        uri: dir.uri,
        get exists() {
          return dir.exists;
        },
        create: () => dir.create(),
        list: () => {
          if (name === 'offline-audio') counts.audioLists += 1;
          return dir.list();
        },
        openFile: (fileName) => countedFile(dir.openFile(fileName)),
      };
    };
    return { store, counts };
  }

  function ids(prefix: string): TrackId[] {
    return Array.from({ length: LIBRARY_SIZE }, (_, i) => asTrackId(`${prefix}${i}`));
  }

  function audioUri(trackId: string): string {
    return `memory://document/offline-audio/${trackId}.mp3`;
  }

  async function settle(): Promise<void> {
    for (let i = 0; i < 20 && usePinnedStore.getState().isWorking; i += 1) {
      await new Promise((resolve) => setImmediate(resolve));
    }
  }

  let memory: MemoryFileStore;
  let counts: Counts;

  beforeEach(() => {
    ({ store: memory, counts } = counting(createMemoryFileStore()));
    setPinnedFileStore(memory);
    setPinnedIndexFileStore(memory);
    usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
    fetchAudioUrlsMock.mockReset();
    fetchAudioUrlsMock.mockImplementation(async ([id]) => [
      {
        trackId: id!,
        url: `https://cdn.example.com/${id}.mp3`,
        version: 'v1',
      } satisfies ResolvedAudioUrl,
    ]);
  });

  afterEach(async () => {
    await settle();
    setPinnedFileStore();
    setPinnedIndexFileStore();
  });

  describe('reconcile on a large pinned library', () => {
    it('lists the pinned directory a bounded number of times and writes the index once, not once per entry', () => {
      const trackIds = ids('r');
      const entries: Record<string, PinnedEntry> = {};
      memory.directories.add('memory://document/offline-audio');
      trackIds.forEach((trackId, i) => {
        // Two in three entries have their file; the rest are gone and resolve to queued or dropped.
        if (i % 3 !== 0)
          memory.files.set(`memory://document/offline-audio/${trackId}.mp3`, 'audio');
        entries[trackId] =
          i % 2 === 0
            ? { trackId, status: 'ready', uri: audioUri(trackId) }
            : { trackId, status: 'downloading' };
      });
      usePinnedStore.setState({ entries, isWorking: true });

      usePinnedStore.getState().reconcile();

      expect(counts.audioLists).toBeLessThanOrEqual(1);
      expect(counts.indexWrites).toBe(1);
      const next = usePinnedStore.getState().entries;
      expect(next['r1']).toEqual({
        trackId: 'r1',
        status: 'ready',
        uri: 'memory://document/offline-audio/r1.mp3',
      });
      expect(next['r0']).toBeUndefined();
      expect(next['r3']).toEqual({ trackId: 'r3', status: 'queued' });
      expect(Object.keys(next)).toHaveLength(LIBRARY_SIZE - LIBRARY_SIZE / 6);
    });
  });

  describe('unpinAll on a large pinned library whose deletes fail', () => {
    it('checks which files survived with one directory listing, not one per entry', () => {
      const trackIds = ids('u');
      const entries: Record<string, PinnedEntry> = {};
      memory.directories.add('memory://document/offline-audio');
      for (const trackId of trackIds) {
        memory.files.set(audioUri(trackId), 'audio');
        entries[trackId] = { trackId, status: 'ready', uri: audioUri(trackId) };
      }
      usePinnedStore.setState({ entries });
      const deleteFile = memory.files.delete.bind(memory.files);
      memory.files.delete = (uri) => {
        if (uri.endsWith('/u7.mp3')) throw new Error('EBUSY: file is locked');
        return deleteFile(uri);
      };
      const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);

      usePinnedStore.getState().unpinAll();

      warn.mockRestore();
      expect(counts.audioLists).toBeLessThanOrEqual(2);
      expect(usePinnedStore.getState().entries).toEqual({
        u7: { trackId: 'u7', status: 'ready', uri: audioUri('u7') },
      });
    });
  });

  describe('unpinAll when the pinned directory becomes unreadable after a failed delete', () => {
    it('keeps no survivors rather than guessing which files remain', () => {
      memory.directories.add('memory://document/offline-audio');
      memory.files.set(audioUri('u1'), 'audio');
      usePinnedStore.setState({
        entries: { u1: { trackId: asTrackId('u1'), status: 'ready', uri: audioUri('u1') } },
      });
      memory.files.delete = () => {
        throw new Error('EBUSY: file is locked');
      };
      const openDirectory = memory.openDirectory;
      memory.openDirectory = (name) => {
        const dir = openDirectory(name);
        return {
          ...dir,
          exists: true,
          create: () => dir.create(),
          openFile: (fileName) => dir.openFile(fileName),
          list: () => {
            if (counts.audioLists >= 1) throw new Error('EIO: listing failed');
            return dir.list();
          },
        };
      };
      const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);

      usePinnedStore.getState().unpinAll();

      warn.mockRestore();
      expect(usePinnedStore.getState().entries).toEqual({});
    });
  });

  describe('bulk removal of a large downloaded selection', () => {
    it('lists the pinned audio directory once per batch of removals, not once per track', async () => {
      const trackIds = ids('b');
      const entries: Record<string, PinnedEntry> = {};
      memory.directories.add('memory://document/offline-audio');
      for (const trackId of trackIds) {
        memory.files.set(audioUri(trackId), 'audio');
        entries[trackId] = { trackId, status: 'ready', uri: audioUri(trackId) };
      }
      usePinnedStore.setState({ entries });

      const result = await usePinnedStore.getState().unpinMany(trackIds);

      expect(result).toEqual({ requested: LIBRARY_SIZE, failed: 0 });
      expect(usePinnedStore.getState().entries).toEqual({});
      // One listing and one index write per batch, so the cost stays linear in the tracks removed
      // rather than quadratic in them (#1699).
      expect(counts.audioLists).toBeLessThanOrEqual(LIBRARY_SIZE / 16);
      expect(counts.indexWrites).toBeLessThanOrEqual(3);
    });
  });

  describe('batch pin of a large library', () => {
    it('writes the index a bounded number of times while every track moves through downloading to ready', async () => {
      const trackIds = ids('p');

      const result = await usePinnedStore.getState().pinMany(trackIds);

      expect(result).toEqual({ requested: LIBRARY_SIZE, failed: 0 });
      await settle();
      expect(usePinnedStore.getState().isWorking).toBe(false);
      expect(counts.indexWrites).toBeLessThanOrEqual(3);
      const persisted = (
        JSON.parse(memory.files.get('memory://document/offline/pinned.json')!) as {
          entries: Record<string, PinnedEntry>;
        }
      ).entries;
      expect(persisted).toEqual(usePinnedStore.getState().entries);
      expect(persisted['p42']).toEqual({
        trackId: 'p42',
        status: 'ready',
        uri: 'memory://document/offline-audio/p42.mp3',
        version: 'v1',
      });
    });

    it('lists the pinned audio directory a bounded number of times, not once per track', async () => {
      const trackIds = ids('l');

      const result = await usePinnedStore.getState().pinMany(trackIds);

      expect(result).toEqual({ requested: LIBRARY_SIZE, failed: 0 });
      await settle();
      expect(counts.audioLists).toBeLessThanOrEqual(3);
    });
  });
});

describe('the remote kill switch stops pinned downloads', () => {
  // Regression for issue #955: the pinned-download worker must stop while its remote kill switch is
  // off, so a download loop thrashing disk can be stopped without an app release.

  function resolved(trackId: string): ResolvedAudioUrl {
    return { trackId, url: `https://cdn.example.com/audio/${trackId}.mp3`, version: 'v1' };
  }

  async function flush(rounds = 30): Promise<void> {
    for (let i = 0; i < rounds; i += 1) await Promise.resolve();
  }

  function statuses(): Record<string, string> {
    const out: Record<string, string> = {};
    for (const [id, entry] of Object.entries(usePinnedStore.getState().entries))
      out[id] = entry.status;
    return out;
  }

  beforeEach(() => {
    setKillSwitchFileStore(createMemoryFileStore());
    usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
    fetchAudioUrlsMock.mockReset().mockImplementation(async (ids) => ids.map(resolved));
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    setKillSwitchFileStore();
    jest.restoreAllMocks();
  });

  describe('pinned downloads — remote kill switch', () => {
    it('downloads nothing while the switch is off and leaves the tracks queued', async () => {
      applyKillSwitches({ offline_downloads_enabled: false });

      await act(async () => {
        usePinnedStore.getState().pin(asTrackId('A'));
        void usePinnedStore.getState().pinMany([asTrackId('B')]);
        await flush();
      });

      expect(fetchAudioUrlsMock).not.toHaveBeenCalled();
      expect(statuses()).toEqual({ A: 'queued', B: 'queued' });
      expect(usePinnedStore.getState().queue).toEqual(['A', 'B']);
      expect(usePinnedStore.getState().isWorking).toBe(false);
    });

    it('does not resume queued tracks on reconcile while the switch is off', async () => {
      applyKillSwitches({ offline_downloads_enabled: false });
      usePinnedStore.setState({ entries: { A: { trackId: asTrackId('A'), status: 'queued' } } });

      await act(async () => {
        usePinnedStore.getState().reconcile();
        await flush();
      });

      expect(fetchAudioUrlsMock).not.toHaveBeenCalled();
      expect(statuses()).toEqual({ A: 'queued' });
    });

    it('stops the drain before the next track when the switch is turned off mid-batch', async () => {
      let releaseFirst!: () => void;
      fetchAudioUrlsMock.mockImplementationOnce(
        (ids) =>
          new Promise((resolve) => {
            releaseFirst = () => resolve(ids.map(resolved));
          }),
      );

      await act(async () => {
        void usePinnedStore.getState().pinMany([asTrackId('A'), asTrackId('B'), asTrackId('C')]);
        await flush();
      });
      expect(fetchAudioUrlsMock).toHaveBeenCalledTimes(1);

      await act(async () => {
        applyKillSwitches({ offline_downloads_enabled: false });
        releaseFirst();
        await flush();
      });

      expect(fetchAudioUrlsMock).toHaveBeenCalledTimes(1);
      expect(statuses()).toEqual({ A: 'ready', B: 'queued', C: 'queued' });
      expect(usePinnedStore.getState().queue).toEqual(['B', 'C']);
    });

    it('resumes the held-back queue when the switch is turned back on', async () => {
      applyKillSwitches({ offline_downloads_enabled: false });
      let batch!: Promise<unknown>;
      await act(async () => {
        batch = usePinnedStore.getState().pinMany([asTrackId('A'), asTrackId('B')]);
        await flush();
      });

      await act(async () => {
        applyKillSwitches({ offline_downloads_enabled: true });
        await flush(60);
      });

      await expect(batch).resolves.toEqual({ requested: 2, failed: 0 });
      expect(fetchAudioUrlsMock.mock.calls.map(([ids]) => ids)).toEqual([['A'], ['B']]);
      expect(statuses()).toEqual({ A: 'ready', B: 'ready' });
    });

    it('has nothing to resume when the switch comes back on with an empty queue', () => {
      applyKillSwitches({ offline_downloads_enabled: false });

      applyKillSwitches({ offline_downloads_enabled: true });

      expect(fetchAudioUrlsMock).not.toHaveBeenCalled();
      expect(usePinnedStore.getState().isWorking).toBe(false);
    });
  });
});

describe('downloads belong to one account and are cleared on sign-out', () => {
  const { __fs } = FileSystem as unknown as {
    __fs: {
      seedFile(uri: string, contents: string): void;
      readFile(uri: string): string | undefined;
      failNext(kind: 'read' | 'delete', error?: Error): void;
    };
  };

  const OWNER_URI = 'file:///document/offline/pinned-owner';
  const AUDIO_URI = 'file:///document/offline-audio/t1.mp3';

  function seedReadyDownload(): void {
    __fs.seedFile(AUDIO_URI, 'audio-bytes');
    usePinnedStore.setState({
      entries: { t1: { trackId: asTrackId('t1'), status: 'ready', uri: AUDIO_URI } },
      queue: [],
      isWorking: false,
    });
  }

  beforeEach(() => {
    usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
  });

  describe('claimPinnedDownloads — downloads belong to the account that made them', () => {
    it('keeps downloads the claiming user already owns', () => {
      __fs.seedFile(OWNER_URI, 'user-a');
      seedReadyDownload();

      claimPinnedDownloads('user-a');

      expect(resolvePinnedUri(asTrackId('t1'))).toBe(AUDIO_URI);
      expect(__fs.readFile(AUDIO_URI)).toBe('audio-bytes');
    });

    it('deletes another account downloads and records the new owner', () => {
      __fs.seedFile(OWNER_URI, 'user-a');
      seedReadyDownload();

      claimPinnedDownloads('user-b');

      expect(resolvePinnedUri(asTrackId('t1'))).toBeUndefined();
      expect(__fs.readFile(AUDIO_URI)).toBeUndefined();
      expect(__fs.readFile(OWNER_URI)).toBe('user-b');
    });

    it('never adopts another account download whose file failed to delete', () => {
      const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
      __fs.seedFile(OWNER_URI, 'user-a');
      seedReadyDownload();
      __fs.failNext('delete', new Error('file is locked'));

      claimPinnedDownloads('user-b');

      expect(resolvePinnedUri(asTrackId('t1'))).toBeUndefined();
      expect(usePinnedStore.getState().entries).toEqual({});
      expect(__fs.readFile(OWNER_URI)).toBe('user-b');
      warn.mockRestore();
    });

    it('treats an unreadable owner record as foreign and deletes the downloads', () => {
      __fs.seedFile(OWNER_URI, 'user-a');
      seedReadyDownload();
      __fs.failNext('read');

      claimPinnedDownloads('user-a');

      expect(resolvePinnedUri(asTrackId('t1'))).toBeUndefined();
      expect(__fs.readFile(AUDIO_URI)).toBeUndefined();
    });

    it('a failed owner write leaves no owner on disk, so the next claim clears again instead of adopting', () => {
      const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
      const realWrite = FileSystem.File.prototype.write;
      const write = jest.spyOn(FileSystem.File.prototype, 'write').mockImplementation(function (
        this: FileSystem.File,
        ...args
      ) {
        if (this.uri === OWNER_URI) throw new Error('ENOSPC');
        return realWrite.apply(this, args);
      });
      seedReadyDownload();

      claimPinnedDownloads('user-a');

      expect(__fs.readFile(OWNER_URI)).toBeUndefined();
      expect(warn).toHaveBeenCalledWith(expect.stringContaining('pinned owner'));
      write.mockRestore();
      warn.mockRestore();
    });
  });

  describe('sign-out cleanup registry', () => {
    it('removes every download when the signed-in identity changes', () => {
      seedReadyDownload();

      runSignOutCleanups();

      expect(usePinnedStore.getState().entries).toEqual({});
      expect(__fs.readFile(AUDIO_URI)).toBeUndefined();
    });
  });
});
