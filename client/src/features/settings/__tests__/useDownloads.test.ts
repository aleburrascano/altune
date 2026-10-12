import { act, renderHook } from '@testing-library/react-native';
import * as FileSystem from 'expo-file-system';

import { pinnedByteTotal, usePinnedStore, type PinnedEntry } from '@shared/offline/pinnedStore';
import { useDownloadStats, useRemoveDownloads } from '@features/settings/hooks/useDownloads';
import { downloadStats } from '@features/settings/domain/downloadStatsModel';
import {
  buildDangerZoneActions,
  type ClearHistoryState,
} from '@features/settings/domain/dangerZoneActions';
import { asTrackId } from '@shared/apiClient/ids';

const { __fs } = FileSystem as unknown as {
  __fs: {
    seedFile(uri: string, contents: string): void;
    readFile(uri: string): string | undefined;
    failNext(kind: 'delete', error?: Error): void;
  };
};

const audioUri = (trackId: string): string => `file:///document/offline-audio/${trackId}.mp3`;

function seedReady(...trackIds: string[]): void {
  const entries: Record<string, PinnedEntry> = {};
  for (const trackId of trackIds) {
    __fs.seedFile(audioUri(trackId), 'audio-bytes');
    entries[trackId] = { trackId: asTrackId(trackId), status: 'ready', uri: audioUri(trackId) };
  }
  usePinnedStore.setState({ entries, queue: [], isWorking: false });
}

function currentStats(): ReturnType<typeof downloadStats> {
  return downloadStats(usePinnedStore.getState().entries, pinnedByteTotal());
}

const clearHistory: ClearHistoryState = {
  mutate: jest.fn(),
  isPending: false,
  isError: false,
  isSuccess: false,
  error: undefined,
};

function removeDownloadsRowHidden(): boolean | undefined {
  const stats = currentStats();
  const [downloads] = buildDangerZoneActions({
    downloads: { stats, unpinAll: jest.fn() },
    signOutState: { status: 'idle' },
    clearHistory,
    signOut: jest.fn(),
  });
  return downloads?.row.hidden;
}

let warn: jest.SpyInstance;

beforeEach(() => {
  usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
  warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  warn.mockRestore();
});

describe('download count and size stay in agreement when a file delete fails', () => {
  it('unpinAll keeps the track whose file could not be deleted, so count and size agree', () => {
    seedReady('t1', 't2');
    __fs.failNext('delete', new Error('file is locked'));

    usePinnedStore.getState().unpinAll();

    const stats = currentStats();
    expect(pinnedByteTotal()).toBeGreaterThan(0);
    expect(stats.downloadCount).toBe(1);
    expect(stats.usageLabel).toBe('1 track');
    expect(removeDownloadsRowHidden()).toBe(false);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('failed to delete'));
  });

  it('a retried unpinAll removes the survivor once the delete succeeds', () => {
    seedReady('t1');
    __fs.failNext('delete', new Error('file is locked'));
    usePinnedStore.getState().unpinAll();

    usePinnedStore.getState().unpinAll();

    expect(usePinnedStore.getState().entries).toEqual({});
    expect(pinnedByteTotal()).toBe(0);
    expect(removeDownloadsRowHidden()).toBe(true);
  });

  it('unpin keeps a ready track whose file could not be deleted instead of forgetting it', () => {
    seedReady('t1');
    __fs.failNext('delete', new Error('file is locked'));

    usePinnedStore.getState().unpin(asTrackId('t1'));

    expect(usePinnedStore.getState().entries['t1']?.status).toBe('ready');
    expect(currentStats().downloadCount).toBe(1);
  });

  it('bytes left on disk with no indexed track still show the retry row and an honest label', () => {
    __fs.seedFile(audioUri('orphan'), 'audio-bytes');

    const stats = currentStats();

    expect(stats.downloadCount).toBe(0);
    expect(stats.usageLabel).toBe('Leftover download files');
    expect(stats.usageDetail).toBe(stats.downloadSize);
    expect(removeDownloadsRowHidden()).toBe(false);

    usePinnedStore.getState().unpinAll();
    expect(__fs.readFile(audioUri('orphan'))).toBeUndefined();
  });
});

describe('useDownloadStats measures the pinned directory only when entries change', () => {
  it('a re-render with the same entries does not re-list the directory', () => {
    seedReady('t1');
    const listing = jest.spyOn(FileSystem.Directory.prototype, 'list');
    const { rerender } = renderHook(() => useDownloadStats());
    const afterMount = listing.mock.calls.length;

    rerender({});
    rerender({});

    expect(listing).toHaveBeenCalledTimes(afterMount);
    listing.mockRestore();
  });
});

describe('downloadStats — counts only ready entries', () => {
  const entry = (trackId: string, status: PinnedEntry['status']): PinnedEntry =>
    ({ trackId, status }) as PinnedEntry;

  it('reports no downloads when nothing is ready, hiding the size detail', () => {
    const stats = downloadStats({ a: entry('a', 'queued'), b: entry('b', 'failed') }, 0);
    expect(stats.downloadCount).toBe(0);
    expect(stats.downloadBytes).toBe(0);
    expect(stats).toEqual({
      downloadCount: 0,
      downloadBytes: 0,
      downloadSize: '0 B',
      usage: 'none',
      usageLabel: 'No downloads on this device',
      usageDetail: undefined,
    });
  });

  it('uses the singular noun for one ready track and shows the formatted size', () => {
    const stats = downloadStats({ a: entry('a', 'ready'), b: entry('b', 'downloading') }, 2048);
    expect(stats.downloadCount).toBe(1);
    expect(stats.usageLabel).toBe('1 track');
    expect(stats.usageDetail).toBe('2.0 KB');
  });

  it('uses the plural noun for several ready tracks', () => {
    const stats = downloadStats({ a: entry('a', 'ready'), b: entry('b', 'ready') }, 5 * 1024 ** 2);
    expect(stats.usageLabel).toBe('2 tracks');
    expect(stats.downloadSize).toBe('5.0 MB');
  });
});

beforeEach(() => {
  usePinnedStore.setState({ entries: {}, queue: [], isWorking: false, lastUnpinAll: undefined });
});

describe('useRemoveDownloads', () => {
  it('groups the given stats with the store unpinAll and no outcome yet', () => {
    const stats = downloadStats({}, 0);
    const { result } = renderHook(() => useRemoveDownloads(stats));

    expect(result.current.stats).toBe(stats);
    expect(result.current.lastUnpinAll).toBeUndefined();
    expect(typeof result.current.unpinAll).toBe('function');
  });

  it('carries the store outcome once a removal has run', () => {
    usePinnedStore.setState({ entries: {}, queue: [], isWorking: false, lastUnpinAll: 'partial' });
    const stats = downloadStats({}, 0);

    const { result } = renderHook(() => useRemoveDownloads(stats));

    expect(result.current.lastUnpinAll).toBe('partial');
  });

  it('calls through to the store unpinAll', () => {
    const stats = downloadStats({}, 0);
    const { result } = renderHook(() => useRemoveDownloads(stats));

    act(() => {
      result.current.unpinAll();
    });

    expect(usePinnedStore.getState().lastUnpinAll).toBe('all-removed');
  });
});

describe('useRemoveDownloads probe: outcomes a caller sees', () => {
  const { __fs } = FileSystem as unknown as {
    __fs: {
      seedFile(uri: string, contents: string): void;
      failNext(kind: 'delete', error?: Error): void;
    };
  };

  let warn: jest.SpyInstance;

  beforeEach(() => {
    warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    warn.mockRestore();
  });

  it('reports a partial outcome when a file survives removal through the hook', () => {
    const uri = 'file:///document/offline-audio/t1.mp3';
    __fs.seedFile(uri, 'audio-bytes');
    usePinnedStore.setState({
      entries: { t1: { trackId: asTrackId('t1'), status: 'ready', uri } },
      queue: [],
      isWorking: false,
      lastUnpinAll: undefined,
    });
    __fs.failNext('delete', new Error('file is locked'));
    const stats = downloadStats({}, 0);
    const { result } = renderHook(() => useRemoveDownloads(stats));

    act(() => {
      result.current.unpinAll();
    });

    expect(result.current.lastUnpinAll).toBe('partial');
  });

  it('picks up an outcome recorded after it mounted', () => {
    const stats = downloadStats({}, 0);
    const { result } = renderHook(() => useRemoveDownloads(stats));
    expect(result.current.lastUnpinAll).toBeUndefined();

    act(() => {
      usePinnedStore.setState({ lastUnpinAll: 'partial' });
    });
    expect(result.current.lastUnpinAll).toBe('partial');

    act(() => {
      usePinnedStore.setState({ lastUnpinAll: 'all-removed' });
    });
    expect(result.current.lastUnpinAll).toBe('all-removed');
  });

  it('hands back the newest stats it is given', () => {
    const before = downloadStats({}, 0);
    const after = downloadStats({}, 5 * 1024 ** 2);
    const { result, rerender } = renderHook(
      ({ stats }: { stats: typeof before }) => useRemoveDownloads(stats),
      { initialProps: { stats: before } },
    );

    rerender({ stats: after });

    expect(result.current.stats).toBe(after);
  });
});
