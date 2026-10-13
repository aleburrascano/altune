import { canPlay } from '@shared/playback/canPlay';
import {
  isCurrentlyPlaying,
  type PlaybackContextValue,
} from '@shared/playback/playbackContextValue';
import type { PlaybackSource } from '@shared/playback/playbackTrack';

import type { TrackExtras } from '@features/detail/domain/trackExtras';
import type { OwnedTrack } from '@features/detail/domain/ownedTrack';

export function resolvePlaySource(
  te: TrackExtras,
  owned: OwnedTrack | null,
): PlaybackSource | null {
  const trackId = te.trackId ?? owned?.trackId ?? null;
  const acquisitionStatus = te.acquisitionStatus ?? owned?.acquisitionStatus ?? null;
  if (canPlay(acquisitionStatus) && trackId !== null) {
    return { kind: 'library', trackId };
  }
  if (te.previewUrl !== null) {
    return { kind: 'preview', previewUrl: te.previewUrl };
  }
  return null;
}

function playSourceCandidates(te: TrackExtras, owned: OwnedTrack | null): PlaybackSource[] {
  const trackId = te.trackId ?? owned?.trackId ?? null;
  const candidates: PlaybackSource[] = [];
  if (trackId !== null) candidates.push({ kind: 'library', trackId });
  if (te.previewUrl !== null) candidates.push({ kind: 'preview', previewUrl: te.previewUrl });
  return candidates;
}

export function isResultPlaying(
  playback: Pick<PlaybackContextValue, 'status' | 'track'>,
  te: TrackExtras,
  owned: OwnedTrack | null,
): boolean {
  return playSourceCandidates(te, owned).some((source) => isCurrentlyPlaying(playback, source));
}
