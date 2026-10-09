import type { TrackId } from '@shared/api-client/ids';
import type { AcquisitionStatus } from '@shared/api-client/trackResponse';

import type { TrackExtras } from '@/features/detail/domain/extras-accessors';

export type OwnedTrack = { trackId: TrackId } & (
  | { acquisitionStatus: 'failed'; failureMessage: string | null; retryable: boolean }
  | { acquisitionStatus: Exclude<AcquisitionStatus, 'failed'> }
);

export function ownedTrack(
  trackId: TrackId,
  acquisitionStatus: AcquisitionStatus,
  failureMessage: string | null,
  retryable = true,
): OwnedTrack {
  return acquisitionStatus === 'failed'
    ? { trackId, acquisitionStatus, failureMessage, retryable }
    : { trackId, acquisitionStatus };
}

export function ownedFromExtras(te: TrackExtras): OwnedTrack | null {
  if (te.trackId === null || te.acquisitionStatus === null) {
    return null;
  }
  return ownedTrack(te.trackId, te.acquisitionStatus, null);
}
