import * as FileSystem from 'expo-file-system';

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
import { resolvePinnedUri, usePinnedStore, type PinnedEntry } from '../pinnedStore';
import { createMemoryFileStore, type MemoryFileStore } from '@shared/files/__tests__/memoryFileStore';
import { asTrackId, type TrackId } from '@shared/api-client/ids';

jest.mock('@shared/api-client/audio', () => ({
  fetchAudioUrls: jest.fn().mockResolvedValue([]),
}));

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
    __fs.seedFile(INDEX_URI, JSON.stringify({ t1: { trackId: asTrackId('t1'), status: 'ready' } }));

    expect(importFreshEntries()).toEqual({});
  });

  it('a failed entry loads back, so a download that failed before the relaunch still offers a retry', () => {
    __fs.seedFile(INDEX_URI, JSON.stringify({ t1: { trackId: asTrackId('t1'), status: 'failed' } }));

    expect(importFreshEntries()).toEqual({ t1: { trackId: asTrackId('t1'), status: 'failed' } });
  });

  it('an entry whose status this version does not know is dropped rather than seeded into store state', () => {
    __fs.seedFile(INDEX_URI, JSON.stringify({ t1: { trackId: asTrackId('t1'), status: 'archived' } }));

    expect(importFreshEntries()).toEqual({});
  });

  it('an entry whose uri is the wrong type is dropped rather than seeded', () => {
    __fs.seedFile(INDEX_URI, JSON.stringify({ t1: { trackId: asTrackId('t1'), status: 'ready', uri: 42 } }));

    expect(importFreshEntries()).toEqual({});
  });

  it('an entry whose version is the wrong type is dropped rather than seeded', () => {
    __fs.seedFile(
      INDEX_URI,
      JSON.stringify({ t1: { trackId: asTrackId('t1'), status: 'ready', uri: `${AUDIO_DIR_URI}/t1.mp3`, version: 7 } }),
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
      JSON.stringify({ bad: null, good: { trackId: asTrackId('good'), status: 'ready', uri: `${AUDIO_DIR_URI}/good.mp3` } }),
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
      JSON.stringify({ t1: { trackId: asTrackId('t2'), status: 'ready', uri: `${AUDIO_DIR_URI}/t2.mp3` } }),
    );

    const entries = importFreshEntries() as Record<string, PinnedEntry>;
    expect(entries['t1']?.trackId).toBe('t2');
    expect(entries['t2']).toBeUndefined();
  });
});

describe('adversarial — a shape the types promise cannot exist', () => {
  function setRawEntries(raw: unknown): void {
    usePinnedStore.setState({ entries: raw as Record<string, PinnedEntry>, queue: [], isWorking: false });
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

      expect(usePinnedStore.getState().entries['']).toEqual({ trackId: '', status: 'downloading' });
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
        entries[`t${i}`] = { trackId: asTrackId(`t${i}`), status: i % 2 === 0 ? 'ready' : 'failed', uri: `${AUDIO_DIR_URI}/t${i}.mp3` };
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
