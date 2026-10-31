import type { PlaylistId } from '@shared/apiClient/ids';
import type { PlaylistDetailResponse } from '@shared/apiClient/playlistResponse';
import { buildPlayableQueue } from '@shared/playback/playFromList';
import type { QueueSource } from '@shared/playback/queueOrder';
import type { useQueuePlayback } from '@shared/playback/useQueuePlayback';

type QueueControls = Pick<ReturnType<typeof useQueuePlayback>, 'playFromList' | 'shuffleFromList'>;

type PlaylistPlayback = {
  play: () => void;
  shuffle: () => void;
  playFrom: (trackId: string) => void;
};

export function usePlaylistPlayback(
  playlistId: PlaylistId,
  playlist: PlaylistDetailResponse | undefined,
  queue: QueueControls,
): PlaylistPlayback {
  function source(name: string): QueueSource {
    return { kind: 'playlist', playlistId, name };
  }

  function startAt(trackId: string): void {
    if (!playlist) return;
    const { playable, startIndex } = buildPlayableQueue(playlist.tracks, trackId);
    if (playable.length === 0) return;
    queue.playFromList(playable, startIndex, source(playlist.name));
  }

  return {
    play: () => {
      startAt(playlist?.tracks[0]?.id ?? '');
    },
    shuffle: () => {
      if (!playlist) return;
      const { playable } = buildPlayableQueue(playlist.tracks, '');
      if (playable.length === 0) return;
      queue.shuffleFromList(playable, source(playlist.name));
    },
    playFrom: (trackId) => {
      startAt(trackId);
    },
  };
}
