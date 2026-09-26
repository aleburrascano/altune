import { act, renderHook } from '@testing-library/react-native';

import { usePinnedStore } from '@shared/offline/pinnedStore';
import { downloadStats } from '../downloadStatsModel';
import { useRemoveDownloads } from '../hooks/useRemoveDownloads';

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
