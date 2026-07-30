import * as FileSystem from 'expo-file-system';

import { pinnedUri, usePinnedStore, type PinnedEntry } from '../pinnedStore';

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

function readyEntry(trackId: string): PinnedEntry {
  return { trackId, status: 'ready', uri: `${AUDIO_DIR_URI}/${trackId}.mp3` };
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

  it('an entry with no uri field loads intact', () => {
    __fs.seedFile(INDEX_URI, JSON.stringify({ t1: { trackId: 't1', status: 'ready' } }));

    expect(importFreshEntries()).toEqual({ t1: { trackId: 't1', status: 'ready' } });
  });

  it('an entry whose status this version does not know is loaded as-is instead of being rejected', () => {
    __fs.seedFile(INDEX_URI, JSON.stringify({ t1: { trackId: 't1', status: 'archived' } }));

    expect(importFreshEntries()).toEqual({ t1: { trackId: 't1', status: 'archived' } });
  });

  it('an entry whose map key and trackId field disagree is loaded under the map key verbatim', () => {
    __fs.seedFile(
      INDEX_URI,
      JSON.stringify({ t1: { trackId: 't2', status: 'ready', uri: `${AUDIO_DIR_URI}/t2.mp3` } }),
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

  it('a null entry does not crash pinnedUri — returns undefined rather than throwing on entry.status', () => {
    setRawEntries({ t1: null });

    expect(() => pinnedUri('t1')).not.toThrow();
    expect(pinnedUri('t1')).toBeUndefined();
  });

  it('a string in place of an entry object does not crash pinnedUri', () => {
    setRawEntries({ t1: 'ready' });

    expect(pinnedUri('t1')).toBeUndefined();
  });

  it('an entry missing the status field entirely does not crash pinnedUri', () => {
    setRawEntries({ t1: { trackId: 't1', uri: `${AUDIO_DIR_URI}/t1.mp3` } });

    expect(pinnedUri('t1')).toBeUndefined();
  });

  it('unpin removes a null entry without throwing, leaving a real neighbor entry intact', () => {
    setRawEntries({ t1: null, t2: readyEntry('t2') });

    expect(() => usePinnedStore.getState().unpin('t1')).not.toThrow();
    expect(usePinnedStore.getState().entries['t1']).toBeUndefined();
    expect(usePinnedStore.getState().entries['t2']).toEqual(readyEntry('t2'));
  });

  describe('the empty-string key', () => {
    it('pin("") creates and reads back a distinct entry under the empty-string key', () => {
      usePinnedStore.getState().pin('');

      expect(usePinnedStore.getState().entries['']).toEqual({ trackId: '', status: 'downloading' });
    });

    it('pinnedUri("") returns the uri for a ready entry stored under the empty-string key', () => {
      setRawEntries({ '': readyEntry('') });

      expect(pinnedUri('')).toBe(readyEntry('').uri);
    });

    it('unpin("") removes only the empty-string entry, leaving a same-shaped real id untouched', () => {
      setRawEntries({ '': readyEntry(''), t1: readyEntry('t1') });

      usePinnedStore.getState().unpin('');

      expect(usePinnedStore.getState().entries['']).toBeUndefined();
      expect(usePinnedStore.getState().entries['t1']).toBeDefined();
    });
  });

  describe('a very large index', () => {
    function buildLargeIndex(size: number): Record<string, PinnedEntry> {
      const entries: Record<string, PinnedEntry> = {};
      for (let i = 0; i < size; i += 1) {
        entries[`t${i}`] = { trackId: `t${i}`, status: i % 2 === 0 ? 'ready' : 'failed', uri: `${AUDIO_DIR_URI}/t${i}.mp3` };
      }
      return entries;
    }

    it('unpinAll clears every one of 5000 entries', () => {
      usePinnedStore.setState({ entries: buildLargeIndex(5000), queue: [], isWorking: false });

      usePinnedStore.getState().unpinAll();

      expect(Object.keys(usePinnedStore.getState().entries)).toHaveLength(0);
    });

    it('pinnedUri finds the right entry in a 5000-entry index without confusing neighbors', () => {
      usePinnedStore.setState({ entries: buildLargeIndex(5000), queue: [], isWorking: false });

      expect(pinnedUri('t2500')).toBe(`${AUDIO_DIR_URI}/t2500.mp3`);
      expect(pinnedUri('t2501')).toBeUndefined();
    });
  });
});

describe('failure injection', () => {
  it('a write failure leaves memory ahead of disk — a relaunch would resurrect the removed track', () => {
    const priorOnDisk = { t1: readyEntry('t1') };
    __fs.seedFile(INDEX_URI, JSON.stringify(priorOnDisk));
    usePinnedStore.setState({ entries: priorOnDisk, queue: [], isWorking: false });
    __fs.failNext('write', new Error('disk full'));

    usePinnedStore.getState().unpin('t1');

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

    usePinnedStore.getState().unpin('t1');

    expect(usePinnedStore.getState().entries['t1']).toBeUndefined();
    expect(__fs.readFile(INDEX_URI)).toBeUndefined();
  });
});
