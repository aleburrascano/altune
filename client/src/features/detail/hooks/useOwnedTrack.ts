import type { TrackId } from '@shared/api-client/ids';
import {
  trackIdentityKey,
  trackIdForIdentityAtCallTime,
  trackStatusAtCallTime,
  useTrackIdForIdentity,
  useTrackStatus,
  type TrackStatus,
} from '@shared/acquisition/trackStatusStore';

import { ownedFromExtras, ownedTrack, type OwnedTrack } from '@/features/detail/domain/ownedTrack';
import type { TrackExtras } from '@/features/detail/domain/trackExtras';

export type TrackIdentity = {
  title: string;
  artist: string | null;
};

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
  const retryable = live.acquisitionStatus === 'failed' ? live.retryable : undefined;
  return ownedTrack(trackId, live.acquisitionStatus, live.failureMessage, retryable);
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
