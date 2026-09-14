import type { TrackId } from '@shared/api-client/ids';
import type { AcquisitionStatus } from '@shared/api-client/types';
import {
  trackIdentityKey,
  useTrackIdForIdentity,
  useTrackStatus,
} from '@shared/acquisition/trackStatusStore';

import type { TrackExtras } from '../extras-accessors';

export type OwnedTrack = {
  trackId: TrackId;
  acquisitionStatus: AcquisitionStatus;
};

export type TrackIdentity = {
  title: string;
  artist: string | null;
};

export function ownedFromExtras(te: TrackExtras): OwnedTrack | null {
  if (te.trackId === null || te.acquisitionStatus === null) {
    return null;
  }
  return { trackId: te.trackId, acquisitionStatus: te.acquisitionStatus };
}

export function useOwnedTrack(te: TrackExtras, identity?: TrackIdentity): OwnedTrack | null {
  return useResolvedOwnedTrack(ownedFromExtras(te), identity);
}

// Shared identity -> trackId -> live-status resolution. When `stamped` is
// present it short-circuits the identity lookup and falls back to the stamped
// status; when it is null (no owning extras, e.g. TrackSaveControl) the answer
// comes purely from the identity's linked live status.
export function useResolvedOwnedTrack(
  stamped: OwnedTrack | null,
  identity?: TrackIdentity,
): OwnedTrack | null {
  const key = identity != null ? trackIdentityKey(identity.title, identity.artist ?? '') : null;
  const linkedId = useTrackIdForIdentity(stamped === null ? key : null);
  const trackId = stamped?.trackId ?? linkedId ?? null;
  const live = useTrackStatus(trackId);

  if (trackId === null) {
    return null;
  }
  if (live) {
    return { trackId, acquisitionStatus: live.acquisitionStatus };
  }
  return stamped;
}
