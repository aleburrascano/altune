import type { TrackId, AcquisitionStatus, DiscoveryResult } from '@shared/catalog';
import type { PlaybackTrack } from '@shared/playback';

import { trackExtras, type TrackExtras } from '@features/detail/core/trackExtras';
import { isFailed, isReady } from '@shared/catalog';

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
  return isFailed(acquisitionStatus)
    ? { trackId, acquisitionStatus, failureMessage, retryable }
    : { trackId, acquisitionStatus };
}

export function ownedFromTrackInfo(te: TrackExtras): OwnedTrack | null {
  if (te.trackId === null || te.acquisitionStatus === null) {
    return null;
  }
  return ownedTrack(te.trackId, te.acquisitionStatus, null);
}

export type PlayableTrack = {
  owned: OwnedTrack;
  discovery: DiscoveryResult;
};

export type OwnedSplit = {
  playable: PlayableTrack[];
  unownedCount: number;
  acquiringCount: number;
};

export function splitOwned(tracks: readonly DiscoveryResult[]): OwnedSplit {
  const playable: PlayableTrack[] = [];
  let unownedCount = 0;
  let acquiringCount = 0;

  for (const discovery of tracks) {
    const owned = ownedFromTrackInfo(trackExtras(discovery.extras));
    if (owned === null) {
      unownedCount += 1;
    } else if (isReady(owned.acquisitionStatus)) {
      playable.push({ owned, discovery });
    } else {
      acquiringCount += 1;
    }
  }

  return { playable, unownedCount, acquiringCount };
}

export function toPlaybackQueue(
  playable: readonly PlayableTrack[],
  fallbackArtist: string | null,
  fallbackArtwork: string | null,
): PlaybackTrack[] {
  return playable.map(({ owned, discovery }) => ({
    source: { kind: 'library' as const, trackId: owned.trackId },
    title: discovery.title,
    artist: discovery.subtitle ?? fallbackArtist ?? '',
    artworkUrl: discovery.image_url ?? fallbackArtwork,
    durationSeconds: trackExtras(discovery.extras).durationSeconds ?? undefined,
  }));
}

export function playButtonState(split: OwnedSplit): { label: string; disabled: boolean } {
  if (split.playable.length === 0) {
    return { label: 'Play', disabled: true };
  }
  if (split.unownedCount > 0 || split.acquiringCount > 0) {
    return { label: `Play ${split.playable.length}`, disabled: false };
  }
  return { label: 'Play', disabled: false };
}
