import type { PlaylistId } from '@shared/api-client/ids';
import type { PlaylistDetailResponse } from '@shared/api-client/types';
import { buildPlayableQueue } from '@shared/playback/playFromList';
import type { useQueuePlayback } from '@shared/playback/useQueuePlayback';

type QueueControls = Pick<ReturnType<typeof useQueuePlayback>, 'playFromList' | 'toggleShuffle'>;

type PlaylistPlayback = {
  play: () => void;
  shuffle: () => void;
  playFrom: (trackId: string) => void;
};

// Builds the playable queue for a playlist and starts it, tagging the queue source
// with the playlist so the player can show where playback came from.
export function usePlaylistPlayback(
  playlistId: PlaylistId,
  playlist: PlaylistDetailResponse | undefined,
  queue: QueueControls,
): PlaylistPlayback {
  const startAt = (trackId: string, pickIndex?: (playableCount: number) => number): boolean => {
    if (!playlist) return false;
    const { playable, startIndex } = buildPlayableQueue(playlist.tracks, trackId);
    if (playable.length === 0) return false;
    const index = pickIndex ? pickIndex(playable.length) : startIndex;
    queue.playFromList(playable, index, { kind: 'playlist', playlistId, name: playlist.name });
    return true;
  };

  return {
    play: () => {
      startAt(playlist?.tracks[0]?.id ?? '');
    },
    shuffle: () => {
      if (startAt('', (count) => Math.floor(Math.random() * count))) queue.toggleShuffle();
    },
    playFrom: (trackId) => {
      startAt(trackId);
    },
  };
}
