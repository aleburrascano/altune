/**
 * resolvePlaySource — the track body's play-source policy, pure so it
 * unit-tests without rendering.
 *
 * The full Track wins when it is in the library and acquired (id/status from
 * the handoff `extras` OR the live library match, so an acquisition-SSE patch
 * upgrades preview → full without a refetch); else the 30s preview; else
 * nothing (Play disabled).
 */

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
