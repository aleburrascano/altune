// Seams carved out of PlaylistDetailScreen (#781): rename, delete, playback and the
// offline menu entry. Each test pins the behavior the screen had inline before.

import { Alert } from 'react-native';

import { act, renderHook } from '@testing-library/react-native';

import { asPlaylistId, asTrackId } from '@shared/api-client/ids';
import type { PlaylistDetailResponse, TrackResponse } from '@shared/api-client/types';
import { usePinnedStore } from '@shared/offline/pinnedStore';

import { usePlaylistDelete } from '../hooks/usePlaylistDelete';
import { usePlaylistOfflineAction } from '../hooks/usePlaylistOfflineAction';
import { usePlaylistPlayback } from '../hooks/usePlaylistPlayback';
import { usePlaylistRename } from '../hooks/usePlaylistRename';

const mockRename = jest.fn();
const mockDelete = jest.fn();
jest.mock('@shared/playlists', () => ({
  useRenamePlaylist: () => ({ mutate: mockRename }),
  useDeletePlaylist: () => ({ mutate: mockDelete }),
}));

const mockConfirm = jest.fn();
jest.mock('@shared/ui/confirmDestructive', () => ({
  confirmDestructive: (opts: unknown) => mockConfirm(opts),
}));

const PLAYLIST_ID = asPlaylistId('pl1');

function track(id: string, status: TrackResponse['acquisition_status'] = 'ready'): TrackResponse {
  return {
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
  };
}

function playlist(tracks: TrackResponse[]): PlaylistDetailResponse {
  return {
    id: PLAYLIST_ID,
    name: 'Road Trip',
    track_count: tracks.length,
    preview_artwork_urls: [],
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    total_duration_seconds: 0,
    tracks,
  };
}

beforeEach(() => {
  jest.clearAllMocks();
});

describe('usePlaylistRename', () => {
  it('does nothing when the playlist is not loaded', () => {
    const { result } = renderHook(() => usePlaylistRename(PLAYLIST_ID, undefined));
    act(() => result.current.startEditing());
    expect(result.current.isEditing).toBe(false);
  });

  it('seeds the edit name and renames with the trimmed value, closing on settle', () => {
    const { result } = renderHook(() => usePlaylistRename(PLAYLIST_ID, 'Old'));
    act(() => result.current.startEditing());
    expect(result.current).toMatchObject({ isEditing: true, editName: 'Old' });

    act(() => result.current.setEditName('  New  '));
    act(() => result.current.confirmRename());
    expect(mockRename).toHaveBeenCalledWith('New', expect.any(Object));
    expect(result.current.isEditing).toBe(true);

    act(() => mockRename.mock.calls[0][1].onSettled());
    expect(result.current.isEditing).toBe(false);
  });

  it.each([['   '], ['Old'], [' Old ']])('closes without renaming for %j', (name) => {
    const { result } = renderHook(() => usePlaylistRename(PLAYLIST_ID, 'Old'));
    act(() => result.current.startEditing());
    act(() => result.current.setEditName(name));
    act(() => result.current.confirmRename());
    expect(mockRename).not.toHaveBeenCalled();
    expect(result.current.isEditing).toBe(false);
  });
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

describe('usePlaylistPlayback', () => {
  const source = { kind: 'playlist', playlistId: PLAYLIST_ID, name: 'Road Trip' };

  function setup(data: PlaylistDetailResponse | undefined) {
    const queue = { playFromList: jest.fn(), toggleShuffle: jest.fn() };
    const { result } = renderHook(() => usePlaylistPlayback(PLAYLIST_ID, data, queue));
    return { queue, controls: result.current };
  }

  it('is a no-op without a playlist or without playable tracks', () => {
    for (const data of [undefined, playlist([track('a', 'failed')])]) {
      const { queue, controls } = setup(data);
      controls.play();
      controls.shuffle();
      expect(queue.playFromList).not.toHaveBeenCalled();
      expect(queue.toggleShuffle).not.toHaveBeenCalled();
    }
  });

  it('plays from the first track and from a chosen track with the playlist source', () => {
    const { queue, controls } = setup(playlist([track('a'), track('b'), track('c')]));
    controls.play();
    expect(queue.playFromList).toHaveBeenLastCalledWith(expect.any(Array), 0, source);
    controls.playFrom('c');
    expect(queue.playFromList).toHaveBeenLastCalledWith(expect.any(Array), 2, source);
    expect(queue.playFromList.mock.calls[1][0]).toHaveLength(3);
  });

  it('shuffle starts at a random playable index then toggles shuffle', () => {
    const random = jest.spyOn(Math, 'random').mockReturnValue(0.99);
    const { queue, controls } = setup(playlist([track('a'), track('b')]));
    controls.shuffle();
    expect(queue.playFromList).toHaveBeenCalledWith(expect.any(Array), 1, source);
    expect(queue.toggleShuffle).toHaveBeenCalledTimes(1);
    random.mockRestore();
  });
});

describe('usePlaylistOfflineAction', () => {
  const pinMany = jest.fn().mockResolvedValue({ requested: 0, failed: 0 });
  const unpinMany = jest.fn().mockResolvedValue({ requested: 0, failed: 0 });

  function menuFor(tracks: TrackResponse[], readyIds: string[]) {
    const entries = Object.fromEntries(
      readyIds.map((id) => [
        id,
        { trackId: asTrackId(id), status: 'ready' as const, uri: `file:///offline-audio/${id}.mp3` },
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
