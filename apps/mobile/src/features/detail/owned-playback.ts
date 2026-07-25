import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { PlaybackTrack } from '@shared/playback/types';

import { trackExtras } from './extras-accessors';
import { ownedFromExtras, type OwnedTrack } from './hooks/useOwnedTrack';

export type PlayableTrack = {
  owned: OwnedTrack;
  result: DiscoveryResult;
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

  for (const result of tracks) {
    const owned = ownedFromExtras(trackExtras(result.extras));
    if (owned === null) {
      unownedCount += 1;
    } else if (owned.acquisitionStatus === 'ready') {
      playable.push({ owned, result });
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
  return playable.map(({ owned, result }) => ({
    source: { kind: 'library' as const, trackId: owned.trackId },
    title: result.title,
    artist: result.subtitle ?? fallbackArtist ?? '',
    artworkUrl: result.image_url ?? fallbackArtwork,
    durationSeconds: trackExtras(result.extras).durationSeconds ?? undefined,
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
