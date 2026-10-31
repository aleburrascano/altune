import type { ReactNode } from 'react';

import { act, renderHook } from '@testing-library/react-native';
import fc from 'fast-check';

import { asPlaylistId, asTrackId } from '@shared/apiClient/ids';
import type { PlaylistDetailResponse } from '@shared/apiClient/playlistResponse';
import type { TrackResponse } from '@shared/apiClient/trackResponse';
import { canPlay } from '@shared/playback/canPlay';
import type { PlaybackTrack } from '@shared/playback/playbackTrack';
import { useQueueStore } from '@shared/playback/queueStore';
import { PlaybackContext } from '@shared/playback/usePlayback';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { makeIdlePlayback, trackResponseFactory } from '@testing/factories';

import { usePlaylistPlayback } from '@features/library/hooks/usePlaylistPlayback';

const INITIAL_STATE = useQueueStore.getState();
const PLAYLIST_ID = asPlaylistId('pl1');
const STATUSES: TrackResponse['acquisition_status'][] = ['ready', 'pending', 'failed'];
const MAX_TRACKS = 30;

beforeEach(() => {
  useQueueStore.setState(INITIAL_STATE, true);
});

function playlistOf(statuses: readonly TrackResponse['acquisition_status'][]) {
  const tracks = statuses.map((status, i) =>
    trackResponseFactory.build({ id: asTrackId(`t${i}`), acquisition_status: status }),
  );
  const playlist: PlaylistDetailResponse = {
    id: PLAYLIST_ID,
    name: 'Road Trip',
    track_count: tracks.length,
    preview_artwork_urls: [],
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    total_duration_seconds: 0,
    tracks,
  };
  return playlist;
}

function playableIds(playlist: PlaylistDetailResponse): string[] {
  return playlist.tracks.filter((t) => canPlay(t.acquisition_status)).map((t) => t.id);
}

function idOf(track: PlaybackTrack | null): string | null {
  return track?.source.kind === 'library' ? track.source.trackId : null;
}

interface WrapperProps {
  children: ReactNode;
}

function IdlePlayback({ children }: WrapperProps) {
  return <PlaybackContext.Provider value={makeIdlePlayback()}>{children}</PlaybackContext.Provider>;
}

function shufflePlaylist(playlist: PlaylistDetailResponse): void {
  const { result } = renderHook(
    () => usePlaylistPlayback(PLAYLIST_ID, playlist, useQueuePlayback()),
    { wrapper: IdlePlayback },
  );
  act(() => {
    result.current.shuffle();
  });
}

function idsHeardInOnePass(): (string | null)[] {
  const queue = useQueueStore.getState();
  const heard = [idOf(queue.currentTrack())];
  for (let step = 1; step < queue.playOrder.length; step++) heard.push(idOf(queue.skipToNext()));
  return heard;
}

describe('playlist Shuffle, through the real queue', () => {
  it('starts at the first position and plays every playable track exactly once', () => {
    fc.assert(
      fc.property(
        fc.array(fc.constantFrom(...STATUSES), { minLength: 1, maxLength: MAX_TRACKS }),
        (statuses) => {
          useQueueStore.setState(INITIAL_STATE, true);
          const playlist = playlistOf(statuses);
          const expected = playableIds(playlist);
          fc.pre(expected.length > 0);

          shufflePlaylist(playlist);

          expect(useQueueStore.getState().currentIndex).toBe(0);
          expect(idsHeardInOnePass().sort()).toEqual([...expected].sort());
        },
      ),
    );
  });
});
