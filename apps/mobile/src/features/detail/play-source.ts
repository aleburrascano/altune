import type { TrackResponse } from '@shared/api-client/types';
import { canPlay } from '@shared/playback/canPlay';
import type { PlaybackSource } from '@shared/playback/types';

import type { TrackExtras } from './extras-accessors';

export function resolvePlaySource(
  te: TrackExtras,
  libraryMatch: TrackResponse | null,
): PlaybackSource | null {
  const trackId = te.trackId ?? libraryMatch?.id ?? null;
  const acquisitionStatus = te.acquisitionStatus ?? libraryMatch?.acquisition_status ?? null;
  if (canPlay(acquisitionStatus) && trackId !== null) {
    return { kind: 'library', trackId };
  }
  if (te.previewUrl !== null) {
    return { kind: 'preview', previewUrl: te.previewUrl };
  }
  return null;
}
