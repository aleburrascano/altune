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

// THE single rule for "is this track owned / what's its live status", used by
// both the detail rows (useOwnedTrack) and the save control (TrackSaveControl).
// Callers MUST pass the row's own owning extras as `stamped` so both paths
// resolve identically — passing `null` when the row is in fact stamped-owned is
// the bug #748 fixed.
//
// The rule:
//  1. If the row carries owning extras (`stamped` non-null), its baked-in
//     trackId is authoritative. Its status is that trackId's live status if the
//     store has one, otherwise the stamped status. The (title, artist) identity
//     link is NOT consulted — a fuzzy, collision-prone key must never override a
//     row's own identity.
//  2. If the row has no owning extras (`stamped` null, e.g. a never-saved
//     search/discography row), the identity link is consulted so a just-saved
//     track surfaces its live status. No link -> not owned.
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
