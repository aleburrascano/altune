import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';
import { asTrackId, type TrackId } from '@shared/api-client/ids';
import type { StoredDirectory, StoredFile } from '@shared/files/fileStore';
import {
  createMemoryFileStore,
  type MemoryFileStore,
} from '@shared/files/__tests__/memoryFileStore';

import { setPinnedFileStore } from '../pinnedFiles';
import { setPinnedIndexFileStore } from '../pinnedIndex';
import { usePinnedStore, type PinnedEntry } from '../pinnedStore';

jest.mock('@shared/api-client/audio', () => ({ fetchAudioUrls: jest.fn() }));

const fetchAudioUrlsMock = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

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
    { trackId: id!, url: `https://cdn.example.com/${id}.mp3`, version: 'v1' } satisfies ResolvedAudioUrl,
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
      if (i % 3 !== 0) memory.files.set(`memory://document/offline-audio/${trackId}.mp3`, 'audio');
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
