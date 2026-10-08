import type { TrackResponse } from '@shared/api-client/types';

import { canPlay } from '@shared/playback/canPlay';
import { toPlaybackTrack } from '@shared/playback/toPlaybackTrack';
import type { PlaybackTrack, QueueSource } from '@shared/playback/types';

export function buildPlayableQueue(
  tracks: readonly TrackResponse[],
  targetTrackId: string,
): { playable: PlaybackTrack[]; startIndex: number } {
  const playable = tracks.filter((t) => canPlay(t.acquisition_status)).map(toPlaybackTrack);
  const startIndex = playable.findIndex(
    (t) => t.source.kind === 'library' && t.source.trackId === targetTrackId,
  );
  return { playable, startIndex: Math.max(0, startIndex) };
}

export type { QueueSource };
