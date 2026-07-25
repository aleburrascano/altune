import type { AcquisitionStatus } from '@shared/api-client/types';
import { useTrackStatus } from '@shared/acquisition/trackStatusStore';

import type { TrackExtras } from '../extras-accessors';

export type OwnedTrack = {
  trackId: string;
  acquisitionStatus: AcquisitionStatus;
};

export function ownedFromExtras(te: TrackExtras): OwnedTrack | null {
  if (te.trackId === null || te.acquisitionStatus === null) {
    return null;
  }
  return { trackId: te.trackId, acquisitionStatus: te.acquisitionStatus };
}

export function useOwnedTrack(te: TrackExtras): OwnedTrack | null {
  const stamped = ownedFromExtras(te);
  const live = useTrackStatus(stamped?.trackId ?? null);
  if (stamped === null) {
    return null;
  }
  return live ? { trackId: stamped.trackId, acquisitionStatus: live.acquisitionStatus } : stamped;
}
