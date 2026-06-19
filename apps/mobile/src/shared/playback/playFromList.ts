import type { TrackResponse } from '@shared/api-client/types';

import { toPlaybackTrack } from './toPlaybackTrack';
import type { PlaybackTrack, QueueSource } from './types';

export function buildPlayableQueue(
  tracks: readonly TrackResponse[],
  targetTrackId: string,
): { playable: PlaybackTrack[]; startIndex: number } {
  const playable = tracks
    .filter((t) => t.acquisition_status === 'ready')
    .map(toPlaybackTrack);
  const startIndex = playable.findIndex(
    (t) => t.source.kind === 'library' && t.source.trackId === targetTrackId,
  );
  return { playable, startIndex: Math.max(0, startIndex) };
}

export type { QueueSource };
