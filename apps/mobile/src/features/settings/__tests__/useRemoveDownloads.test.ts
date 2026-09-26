import { act, renderHook } from '@testing-library/react-native';

import { usePinnedStore } from '@shared/offline/pinnedStore';
import { downloadStats } from '../downloadStatsModel';
import { useRemoveDownloads } from '../hooks/useRemoveDownloads';
import * as FileSystem from 'expo-file-system';
import { asTrackId } from '@shared/api-client/ids';

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
