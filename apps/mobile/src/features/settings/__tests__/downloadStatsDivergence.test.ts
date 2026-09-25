import { renderHook } from '@testing-library/react-native';
import * as FileSystem from 'expo-file-system';


import { pinnedByteTotal, usePinnedStore, type PinnedEntry } from '@shared/offline/pinnedStore';
import { downloadStats, useDownloadStats } from '../hooks/useDownloadStats';
import { buildDangerZoneActions } from '../ui/dangerZoneActions';
import { asTrackId } from '@shared/api-client/ids';

jest.mock('@shared/offline/pinnedStore', () => {
  const actual = jest.requireActual('@shared/offline/pinnedStore');
  return { ...actual, pinnedByteTotal: jest.fn(actual.pinnedByteTotal) };
});

jest.mock('@shared/api-client/audio', () => ({
  fetchAudioUrls: jest.fn().mockResolvedValue([]),
}));

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

function removeDownloadsRowHidden(): boolean | undefined {
  const stats = currentStats();
  const [downloads] = buildDangerZoneActions({
    ...stats,
    signOutState: { status: 'idle' },
    clearHistory: {} as Parameters<typeof buildDangerZoneActions>[0]['clearHistory'],
    unpinAll: jest.fn(),
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

describe('download count and size stay in agreement when a file delete fails (#837)', () => {
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
    const measure = jest.mocked(pinnedByteTotal);
    measure.mockClear();
    const { rerender } = renderHook(() => useDownloadStats());
    const afterMount = measure.mock.calls.length;

    rerender({});
    rerender({});

    expect(measure).toHaveBeenCalledTimes(afterMount);
  });
});
