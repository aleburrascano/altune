import { renderHook } from '@testing-library/react-native';
import { asTrackId } from '@shared/catalog';
import { usePinnedStore } from '@shared/offline';

import { useLibraryOffline, usePinnedStatus } from '@features/library/view/useLibraryOffline';

describe('useLibraryOffline on web — offline downloads do not exist here', () => {
  it('reports offline downloads unsupported on web', () => {
    const { result } = renderHook(() => useLibraryOffline());
    expect(result.current.supported).toBe(false);
  });
});

describe('useLibraryOffline on web — statusOf and usePinnedStatus stay silent', () => {
  const readyEntries = {
    'track-1': { trackId: asTrackId('track-1'), status: 'ready' as const, uri: 'file:///a' },
  };

  afterEach(() => usePinnedStore.setState({ entries: {} }));

  it('statusOf reports no status on web even with a ready pinned entry in the store', () => {
    usePinnedStore.setState({ entries: readyEntries });
    const { result } = renderHook(() => useLibraryOffline());
    expect(result.current.statusOf(asTrackId('track-1'))).toBeUndefined();
  });

  it('usePinnedStatus reports no status on web even with a ready pinned entry in the store', () => {
    usePinnedStore.setState({ entries: readyEntries });
    const { result } = renderHook(() => usePinnedStatus(asTrackId('track-1')));
    expect(result.current).toBeUndefined();
  });
});
