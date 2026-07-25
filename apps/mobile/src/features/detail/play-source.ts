import { canPlay } from '@shared/playback/canPlay';
import type { PlaybackSource } from '@shared/playback/types';

import type { TrackExtras } from './extras-accessors';
import type { OwnedTrack } from './hooks/useOwnedTrack';

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
