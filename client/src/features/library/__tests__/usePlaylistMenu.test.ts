import { renderHook, act } from '@testing-library/react-native';
import { asPlaylistId, asTrackId } from '@shared/api-client/ids';
import type { Navigator } from '@shared/navigation';
import { createMemoryNavigator } from '@shared/navigation/__tests__/memoryNavigator';
import {
  usePlaylistDelete,
  usePlaylistOfflineAction,
} from '@features/library/hooks/usePlaylistMenu';
import { Alert } from 'react-native';
import type { TrackResponse } from '@shared/api-client/trackResponse';
import { usePinnedStore } from '@shared/offline/pinnedStore';
import { trackResponseFactory } from '@testing/factories';

const mockDelete = jest.fn();

jest.mock('@shared/playlists', () => ({
  useDeletePlaylist: () => ({ mutate: mockDelete }),
}));

const mockConfirm = jest.fn();

jest.mock('@shared/ui/dialog/dialog', () => ({
  ...jest.requireActual('@shared/ui/dialog/dialog'),
  confirmDestructive: (opts: unknown) => mockConfirm(opts),
}));

describe('usePlaylistDelete', () => {
  const PLAYLIST_ID = asPlaylistId('pl1');

  beforeEach(() => {
    jest.clearAllMocks();
  });

  describe('usePlaylistDelete', () => {
    function run(canGoBack: boolean) {
      const router = {
        canGoBack: () => canGoBack,
        back: jest.fn(),
        replace: jest.fn(),
        push: jest.fn(),
      };
      const { result } = renderHook(() => usePlaylistDelete(PLAYLIST_ID, router));
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
});

describe('usePlaylistOfflineAction', () => {
  function track(id: string, status: TrackResponse['acquisition_status'] = 'ready'): TrackResponse {
    return trackResponseFactory.build({
      id: asTrackId(id),
      title: `Track ${id}`,
      artist: 'An Artist',
      album: null,
      duration_seconds: 180,
      added_at: '2024-01-01T00:00:00Z',
      acquisition_status: status,
      artwork_url: null,
      failure_reason: null,
      year: null,
      genre: null,
      track_number: null,
      album_artist: null,
      isrc: null,
      audio_ref: null,
    });
  }

  beforeEach(() => {
    jest.clearAllMocks();
  });

  describe('usePlaylistOfflineAction', () => {
    const pinMany = jest.fn().mockResolvedValue({ requested: 0, failed: 0 });
    const unpinMany = jest.fn().mockResolvedValue({ requested: 0, failed: 0 });

    function menuFor(tracks: TrackResponse[], readyIds: string[]) {
      const entries = Object.fromEntries(
        readyIds.map((id) => [
          id,
          {
            trackId: asTrackId(id),
            status: 'ready' as const,
            uri: `file:///offline-audio/${id}.mp3`,
          },
        ]),
      );
      usePinnedStore.setState({ entries, pinMany, unpinMany });
      return renderHook(() => usePlaylistOfflineAction(tracks)).result.current;
    }

    it('has nothing to download when no track is ready', () => {
      const item = menuFor([track('a', 'failed')], []);
      expect(item.label).toBe('Nothing to download yet');
      item.onPress();
      expect(pinMany).not.toHaveBeenCalled();
    });

    it('downloads all ready tracks when none are pinned', () => {
      const item = menuFor([track('a'), track('b'), track('c', 'failed')], []);
      expect(item.label).toBe('Download all (2)');
      item.onPress();
      expect(pinMany).toHaveBeenCalledWith(['a', 'b']);
    });

    it('downloads the rest when some are pinned', () => {
      const item = menuFor([track('a'), track('b'), track('c')], ['a']);
      expect(item.label).toBe('Download rest (2)');
    });

    it('removes every download in one bulk call when every ready track is pinned', () => {
      const item = menuFor([track('a'), track('b'), track('c', 'failed')], ['a', 'b']);
      expect(item.label).toBe('Remove downloads');
      item.onPress();
      expect(unpinMany.mock.calls).toEqual([[['a', 'b']]]);
    });

    it('summarizes a partial removal as "N of M downloads could not be removed"', async () => {
      const alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {});
      unpinMany.mockResolvedValueOnce({ requested: 2, failed: 1 });
      const item = menuFor([track('a'), track('b')], ['a', 'b']);
      item.onPress();
      await act(async () => {
        await new Promise<void>((resolve) => setImmediate(resolve));
      });
      expect(alert).toHaveBeenCalledTimes(1);
      expect(alert.mock.calls[0]?.[1]).toContain('1 of 2 downloads could not be removed');
      alert.mockRestore();
    });

    it('summarizes a mixed batch as "N of M downloads failed" once it settles', async () => {
      const alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {});
      pinMany.mockResolvedValueOnce({ requested: 2, failed: 1 });
      const item = menuFor([track('a'), track('b')], []);
      item.onPress();
      await act(async () => {
        await new Promise<void>((resolve) => setImmediate(resolve));
      });
      expect(alert).toHaveBeenCalledTimes(1);
      expect(alert.mock.calls[0]?.[1]).toContain('1 of 2 downloads failed');
      alert.mockRestore();
    });
  });
});
