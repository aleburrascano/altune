// A seam carved out of PlaylistDetailScreen (#781): the playlist offline menu entry. Each test
// pins the behavior the screen had inline before.

import { Alert } from 'react-native';

import { act, renderHook } from '@testing-library/react-native';

import { asTrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';
import { usePinnedStore } from '@shared/offline/pinnedStore';

import { usePlaylistOfflineAction } from '../hooks/usePlaylistOfflineAction';

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
