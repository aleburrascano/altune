// A seam carved out of PlaylistDetailScreen (#781): the playlist playback. Each test
// pins the behavior the screen had inline before.

import { renderHook } from '@testing-library/react-native';

import { asPlaylistId, asTrackId } from '@shared/api-client/ids';
import type { PlaylistDetailResponse, TrackResponse } from '@shared/api-client/types';

import { usePlaylistPlayback } from '../hooks/usePlaylistPlayback';

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
