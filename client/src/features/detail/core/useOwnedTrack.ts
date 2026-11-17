import type { TrackId } from '@shared/catalog';
import {
  trackIdentityKey,
  trackIdForIdentityAtCallTime,
  trackStatusAtCallTime,
  useTrackIdForIdentity,
  useTrackStatus,
  type TrackStatus,
} from '@shared/acquisition';

import {
  ownedFromTrackInfo,
  ownedTrack,
  type OwnedTrack,
} from '@features/detail/domain/ownedTrack';
import type { TrackExtras } from '@features/detail/domain/trackExtras';

export type TrackIdentity = {
  title: string;
  artist: string | null;
};

export function useOwnedTrack(te: TrackExtras, identity?: TrackIdentity): OwnedTrack | null {
  return useResolvedOwnedTrack(ownedFromTrackInfo(te), identity);
}

type LiveStatusFields = {
  acquisitionStatus: TrackStatus['acquisitionStatus'];
  failureMessage: TrackStatus['failureMessage'];
  retryable?: boolean;
};

function ownedFromLiveStatus(
  stamped: OwnedTrack | null,
  trackId: TrackId | null,
  live: TrackStatus | undefined,
): OwnedTrack | null {
  if (trackId === null) return null;
  if (!live) return stamped;
  const { acquisitionStatus, failureMessage, retryable }: LiveStatusFields = live;
  return ownedTrack(trackId, acquisitionStatus, failureMessage, retryable);
}

function identityKeyOf(identity: TrackIdentity | undefined): string | null {
  if (identity?.artist == null) return null;
  return trackIdentityKey(identity.title, identity.artist);
}

export function useResolvedOwnedTrack(
  stamped: OwnedTrack | null,
  identity?: TrackIdentity,
): OwnedTrack | null {
  const linkedId = useTrackIdForIdentity(identityKeyOf(identity));
  const trackId = stamped?.trackId ?? linkedId ?? null;
  const live = useTrackStatus(trackId);
  return ownedFromLiveStatus(stamped, trackId, live);
}

export function resolveOwnedTrackAtActionTime(
  stamped: OwnedTrack | null,
  identity?: TrackIdentity,
): OwnedTrack | null {
  const linkedId = trackIdForIdentityAtCallTime(identityKeyOf(identity));
  const trackId = stamped?.trackId ?? linkedId ?? null;
  const live = trackStatusAtCallTime(trackId);
  return ownedFromLiveStatus(stamped, trackId, live);
}
