import { asTrackId, type TrackId } from '@shared/api-client/ids';
import {
  createMemoryFileStore,
  type MemoryFileStore,
} from '@shared/files/__tests__/memoryFileStore';

import { setPinnedFileStore } from '../pinnedFiles';
import { setPinnedIndexFileStore } from '../pinnedIndex';
import { usePinnedStore, type PinnedEntry } from '../pinnedStore';

jest.mock('@shared/api-client/audio', () => ({ fetchAudioUrls: jest.fn() }));

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
