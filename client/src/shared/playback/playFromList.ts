import type { TrackResponse } from '@shared/catalog';
import { canPlay } from '@shared/playback/core/canPlay';
import { toPlaybackTrack, type PlaybackTrack } from '@shared/playback/playbackTrack';

export function buildPlayableQueue(
  tracks: readonly TrackResponse[],
  targetTrackId: string,
): { playable: PlaybackTrack[]; startIndex: number } {
  const playable: PlaybackTrack[] = [];
  for (const t of tracks) {
    if (canPlay(t.acquisition_status)) playable.push(toPlaybackTrack(t));
  }
  const startIndex = playable.findIndex(
    (t) => t.source.kind === 'library' && t.source.trackId === targetTrackId,
  );
  return { playable, startIndex: Math.max(0, startIndex) };
}
