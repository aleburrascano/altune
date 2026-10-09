import { renderHook } from '@testing-library/react-native';

import { asPlaylistId } from '@shared/api-client/ids';
import type { Navigator } from '@shared/navigation';
import { createMemoryNavigator } from '@shared/navigation/__tests__/memoryNavigator';

import { usePlaylistDelete } from '../hooks/usePlaylistDelete';

const mockDelete = jest.fn();
jest.mock('@shared/playlists', () => ({
  useDeletePlaylist: () => ({ mutate: mockDelete }),
}));

const mockConfirm = jest.fn();
jest.mock('@shared/ui/confirmDestructive', () => ({
  confirmDestructive: (opts: unknown) => mockConfirm(opts),
}));

const PLAYLIST_ID = asPlaylistId('pl1');

beforeEach(() => {
  jest.clearAllMocks();
});

describe('usePlaylistDelete', () => {
  function run(canGoBack: boolean) {
    const router = { canGoBack: () => canGoBack, back: jest.fn(), replace: jest.fn() };
    const { result } = renderHook(() =>
      usePlaylistDelete(PLAYLIST_ID, router as unknown as Parameters<typeof usePlaylistDelete>[1]),
    );
    result.current();
    expect(mockConfirm).toHaveBeenCalledWith(
      expect.objectContaining({ title: 'Delete Playlist', confirmLabel: 'Delete' }),
    );
    expect(mockDelete).not.toHaveBeenCalled();
    mockConfirm.mock.calls[0][0].onConfirm();
    mockDelete.mock.calls[0][1].onSuccess();
    return router;
  }

  it('goes back after deleting when there is history', () => {
    const router = run(true);
    expect(router.back).toHaveBeenCalled();
    expect(router.replace).not.toHaveBeenCalled();
  });

  it('replaces with the library after deleting when there is no history', () => {
    const router = run(false);
    expect(router.replace).toHaveBeenCalledWith('/library');
    expect(router.back).not.toHaveBeenCalled();
  });
});

describe('usePlaylistDelete with a memory navigator', () => {
  function deleteThrough(navigator: Navigator) {
    const { result } = renderHook(() => usePlaylistDelete(PLAYLIST_ID, navigator));
    result.current();
    mockConfirm.mock.calls[0][0].onConfirm();
    mockDelete.mock.calls[0][1].onSuccess();
  }

  it('pops back to the previous screen when history exists', () => {
    const navigator = createMemoryNavigator('/library');
    navigator.push('/library/playlist/pl1');
    deleteThrough(navigator);
    expect(navigator.current()).toBe('/library');
  });

  it('lands on the library when there is no history', () => {
    const navigator = createMemoryNavigator('/library/playlist/pl1');
    deleteThrough(navigator);
    expect(navigator.current()).toBe('/library');
  });
});
