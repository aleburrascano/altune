import * as FileSystem from 'expo-file-system';

import { usePinnedStore, type PinnedEntry } from '../pinnedStore';

jest.mock('@shared/api-client/audio', () => ({
  fetchAudioUrls: jest.fn().mockResolvedValue([]),
}));

const { __fs } = FileSystem as unknown as {
  __fs: {
    reset(): void;
    seedFile(uri: string, contents: string): void;
    readFile(uri: string): string | undefined;
    allFiles(): Record<string, string>;
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
  return JSON.parse(raw) as Record<string, PinnedEntry>;
}

function readyEntry(trackId: string): PinnedEntry {
  return { trackId, status: 'ready', uri: `file:///document/offline-audio/${trackId}.mp3` };
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
  it('the on-disk index reflects the entry produced by the synchronous cascade', () => {
    usePinnedStore.getState().pin('t1');

    expect(readIndex()).toEqual({ t1: { trackId: 't1', status: 'downloading' } });
  });

  it('persists the queued entry itself when a download is already in flight, so no mark follows to write it', () => {
    usePinnedStore.setState({ entries: {}, queue: [], isWorking: true });

    usePinnedStore.getState().pin('t1');

    expect(readIndex()).toEqual({ t1: { trackId: 't1', status: 'queued' } });
  });
});

describe('pinMany — writes through to disk, not just to memory', () => {
  it('the on-disk index matches in-memory state exactly, for every fresh id', () => {
    usePinnedStore.getState().pinMany(['t1', 't2']);

    expect(readIndex()).toEqual(usePinnedStore.getState().entries);
  });

  it('persists every queued id of a batch when a download is already in flight, so no mark follows to write them', () => {
    usePinnedStore.setState({ entries: {}, queue: [], isWorking: true });

    usePinnedStore.getState().pinMany(['t1', 't2']);

    expect(readIndex()).toEqual({
      t1: { trackId: 't1', status: 'queued' },
      t2: { trackId: 't2', status: 'queued' },
    });
  });
});

describe('unpin — writes the removal through to disk', () => {
  it('a removed track is gone from the on-disk index, and its sibling survives untouched', () => {
    const seed = { t1: readyEntry('t1'), t2: readyEntry('t2') };
    __fs.seedFile(INDEX_URI, JSON.stringify(seed));
    usePinnedStore.setState({ entries: seed, queue: [], isWorking: false });

    usePinnedStore.getState().unpin('t1');

    expect(readIndex()).toEqual({ t2: seed.t2 });
  });
});

describe('unpinAll — writes an empty index to disk', () => {
  it('the on-disk index becomes exactly {}, not merely emptied in memory', () => {
    const seed = { t1: { trackId: 't1' as const, status: 'queued' as const } };
    __fs.seedFile(INDEX_URI, JSON.stringify(seed));
    usePinnedStore.setState({ entries: seed, queue: ['t1'], isWorking: false });

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
      entries: { t3: { trackId: 't3', status: 'downloading' } },
      queue: [],
      isWorking: true,
    });

    usePinnedStore.getState().unpinAll();

    expect(__fs.allFiles()[audioUri('t3')]).toBeUndefined();
  });
});

describe('relaunch — what one session persists is exactly what a fresh module import loads back', () => {
  it('a session left with entries queued and downloading survives a fresh import unchanged', () => {
    usePinnedStore.getState().pin('t1');
    usePinnedStore.getState().pinMany(['t2', 't3']);
    const persistedBeforeRelaunch = readIndex();

    let reloadedEntries: Record<string, PinnedEntry> | undefined;
    jest.isolateModules(() => {
      const mod = require('../pinnedStore') as { usePinnedStore: typeof usePinnedStore };
      reloadedEntries = mod.usePinnedStore.getState().entries;
    });

    expect(reloadedEntries).toEqual(persistedBeforeRelaunch);
  });

  it('a session left fully unpinned survives a fresh import as an empty index', () => {
    usePinnedStore.getState().pin('t1');
    usePinnedStore.getState().unpin('t1');

    let reloadedEntries: Record<string, PinnedEntry> | undefined;
    jest.isolateModules(() => {
      const mod = require('../pinnedStore') as { usePinnedStore: typeof usePinnedStore };
      reloadedEntries = mod.usePinnedStore.getState().entries;
    });

    expect(reloadedEntries).toEqual({});
  });
});
