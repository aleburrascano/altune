/**
 * "Play what you own" — the shared rule behind the album and artist hero Play.
 *
 * An album or artist detail screen lists DISCOVERY results: provider metadata
 * with no audio behind it. Only tracks you have saved AND whose server-side
 * acquisition finished (`ready`) can actually play — `buildPlayableQueue` filters
 * to exactly those. So a naive "Play" on an album you don't own queues nothing
 * and looks broken, which is why it was never shipped.
 *
 * This resolves the displayed list against the library cache, in display order,
 * and returns only the tracks that will really play. The caller can then offer
 * Play over that set and "Save the rest" over the remainder — never a button
 * that silently does nothing.
 */
import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { TrackResponse } from '@shared/api-client/types';

/** Resolves a displayed title/subtitle to the library row, if any. Matches
 *  `findTrackInLibraryCache`'s signature so callers pass it straight through. */
export type LibraryLookup = (title: string, subtitle: string | null) => TrackResponse | null;

export type OwnedSplit = {
  /** Owned, acquired, in the order shown — the queue Play starts. */
  playable: TrackResponse[];
  /** Displayed tracks with no playable library row yet. */
  unownedCount: number;
  /** Saved but still acquiring server-side: neither playable nor worth
   *  re-saving, so it counts as neither side of the offer. */
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

/** The Play button's label + enabled state for a split. Kept next to the split
 *  so the copy can't drift from the rule that produced it. */
export function playButtonState(split: OwnedSplit): { label: string; disabled: boolean } {
  if (split.playable.length === 0) {
    return { label: 'Play', disabled: true };
  }
  // Naming the count is the honest signal that this is a partial album — a bare
  // "Play" on 3 of 12 tracks reads as a bug when it ends early.
  if (split.unownedCount > 0 || split.acquiringCount > 0) {
    return { label: `Play ${split.playable.length}`, disabled: false };
  }
  return { label: 'Play', disabled: false };
}
