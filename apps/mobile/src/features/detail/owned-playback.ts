import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { TrackResponse } from '@shared/api-client/types';

export type LibraryLookup = (title: string, subtitle: string | null) => TrackResponse | null;

export type OwnedSplit = {
  playable: TrackResponse[];
  unownedCount: number;
  acquiringCount: number;
};

export function splitOwned(tracks: readonly DiscoveryResult[], lookup: LibraryLookup): OwnedSplit {
  const playable: TrackResponse[] = [];
  let unownedCount = 0;
  let acquiringCount = 0;

  for (const track of tracks) {
    const owned = lookup(track.title, track.subtitle);
    if (owned === null) {
      unownedCount += 1;
    } else if (owned.acquisition_status === 'ready') {
      playable.push(owned);
    } else {
      acquiringCount += 1;
    }
  }

  return { playable, unownedCount, acquiringCount };
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
