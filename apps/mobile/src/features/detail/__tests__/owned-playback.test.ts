import { playButtonState, splitOwned, type LibraryLookup } from '../owned-playback';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { TrackResponse } from '@shared/api-client/types';

const listed = (title: string): DiscoveryResult =>
  ({ title, subtitle: 'Radiohead', kind: 'track', sources: [], extras: {} }) as unknown as DiscoveryResult;

const owned = (title: string, status: TrackResponse['acquisition_status']): TrackResponse =>
  ({ id: `id-${title}`, title, artist: 'Radiohead', acquisition_status: status }) as TrackResponse;

/** Library lookup backed by a title→row map. */
const lookupOf = (rows: Record<string, TrackResponse>): LibraryLookup => (title) => rows[title] ?? null;

describe('splitOwned', () => {
  it('keeps playable tracks in the order they are displayed, not library order', () => {
    const tracks = [listed('a'), listed('b'), listed('c')];
    const lookup = lookupOf({
      c: owned('c', 'ready'),
      a: owned('a', 'ready'),
      b: owned('b', 'ready'),
    });

    const split = splitOwned(tracks, lookup);

    expect(split.playable.map((t) => t.title)).toEqual(['a', 'b', 'c']);
  });

  // The three states are genuinely different: unowned needs saving, acquiring
  // needs waiting, ready can play. Collapsing any two produces a wrong offer.
  it('separates unowned, still-acquiring and playable', () => {
    const tracks = [listed('a'), listed('b'), listed('c'), listed('d')];
    const lookup = lookupOf({
      a: owned('a', 'ready'),
      b: owned('b', 'pending'),
      c: owned('c', 'failed'),
    });

    const split = splitOwned(tracks, lookup);

    expect(split.playable.map((t) => t.title)).toEqual(['a']);
    expect(split.acquiringCount).toBe(2); // pending + failed: saved, not playable
    expect(split.unownedCount).toBe(1); // d
  });

  it('reports an album you own none of', () => {
    const split = splitOwned([listed('a'), listed('b')], () => null);

    expect(split.playable).toEqual([]);
    expect(split.unownedCount).toBe(2);
  });
});

describe('playButtonState', () => {
  it('disables Play when nothing can actually play', () => {
    expect(playButtonState({ playable: [], unownedCount: 12, acquiringCount: 0 })).toEqual({
      label: 'Play',
      disabled: true,
    });
  });

  // A bare "Play" over 3 of 12 tracks reads as a bug when the queue ends early;
  // naming the count sets the expectation up front.
  it('names the count when the album is only partly owned', () => {
    expect(
      playButtonState({ playable: [owned('a', 'ready')], unownedCount: 11, acquiringCount: 0 }),
    ).toEqual({ label: 'Play 1', disabled: false });
  });

  it('is a plain Play once everything listed is playable', () => {
    expect(
      playButtonState({
        playable: [owned('a', 'ready'), owned('b', 'ready')],
        unownedCount: 0,
        acquiringCount: 0,
      }),
    ).toEqual({ label: 'Play', disabled: false });
  });
});
