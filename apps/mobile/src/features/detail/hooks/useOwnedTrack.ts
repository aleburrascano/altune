import type { TrackId } from '@shared/api-client/ids';
import type { AcquisitionStatus } from '@shared/api-client/types';
import {
  trackIdentityKey,
  trackIdForIdentityAtCallTime,
  trackStatusAtCallTime,
  useTrackIdForIdentity,
  useTrackStatus,
  type TrackStatus,
} from '@shared/acquisition/trackStatusStore';

import type { TrackExtras } from '../extras-accessors';

// Only a failed track has a reason, so only that arm carries one: a caller must
// narrow on the status before it can read the message, and no other status can
// be built carrying a stale one.
export type OwnedTrack = { trackId: TrackId } & (
  | { acquisitionStatus: 'failed'; failureMessage: string | null }
  | { acquisitionStatus: Exclude<AcquisitionStatus, 'failed'> }
);

export type TrackIdentity = {
  title: string;
  artist: string | null;
};

export function ownedTrack(
  trackId: TrackId,
  acquisitionStatus: AcquisitionStatus,
  failureMessage: string | null,
): OwnedTrack {
  if (acquisitionStatus === 'failed') {
    return { trackId, acquisitionStatus, failureMessage };
  }
  return { trackId, acquisitionStatus };
}

export function ownedFromExtras(te: TrackExtras): OwnedTrack | null {
  if (te.trackId === null || te.acquisitionStatus === null) {
    return null;
  }
  // A row's stamped extras carry a status but never the reason behind it; the
  // live status store is the only place a failure message comes from.
  return ownedTrack(te.trackId, te.acquisitionStatus, null);
}

export function useOwnedTrack(te: TrackExtras, identity?: TrackIdentity): OwnedTrack | null {
  return useResolvedOwnedTrack(ownedFromExtras(te), identity);
}

function ownedFromLiveStatus(
  stamped: OwnedTrack | null,
  trackId: TrackId | null,
  live: TrackStatus | undefined,
): OwnedTrack | null {
  if (trackId === null) return null;
  if (!live) return stamped;
  return ownedTrack(trackId, live.acquisitionStatus, live.failureMessage);
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
  return ownedFromLiveStatus(stamped, trackId, live);
}

export function resolveOwnedTrackAtActionTime(
  stamped: OwnedTrack | null,
  identity?: TrackIdentity,
): OwnedTrack | null {
  const key = identity != null ? trackIdentityKey(identity.title, identity.artist ?? '') : null;
  const linkedId = trackIdForIdentityAtCallTime(stamped === null ? key : null);
  const trackId = stamped?.trackId ?? linkedId ?? null;
  const live = trackStatusAtCallTime(trackId);
  return ownedFromLiveStatus(stamped, trackId, live);
}
