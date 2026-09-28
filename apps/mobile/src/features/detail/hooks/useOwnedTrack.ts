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
