import { playButtonState, splitOwned, toPlaybackQueue } from '../owned-playback';

import type { DiscoveryResult } from '@shared/api-client/discovery';

function trackResult(title: string, extras: Record<string, unknown> = {}): DiscoveryResult {
  return {
    kind: 'track',
    title,
    subtitle: 'Artist',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras,
  };
}

const ownedReady = (id: string) => ({
  owned_track_id: id,
  owned_acquisition_status: 'ready',
});

describe('splitOwned', () => {
  it('counts a result the server did not stamp as unowned', () => {
    const split = splitOwned([trackResult('Unsaved')]);

    expect(split.playable).toHaveLength(0);
    expect(split.unownedCount).toBe(1);
    expect(split.acquiringCount).toBe(0);
  });

  it('keeps a stamped ready track playable, paired with its result', () => {
    const split = splitOwned([trackResult('Saved', ownedReady('track-1'))]);

    expect(split.playable).toHaveLength(1);
    expect(split.playable[0]?.owned.trackId).toBe('track-1');
    expect(split.playable[0]?.result.title).toBe('Saved');
  });

  it('separates acquiring tracks from both playable and unowned', () => {
    const split = splitOwned([
      trackResult('Pending', {
        owned_track_id: 'track-2',
        owned_acquisition_status: 'pending',
      }),
    ]);

    expect(split.playable).toHaveLength(0);
    expect(split.unownedCount).toBe(0);
    expect(split.acquiringCount).toBe(1);
  });

  it('preserves display order in the playable list', () => {
    const split = splitOwned([
      trackResult('First', ownedReady('a')),
      trackResult('Skipped'),
      trackResult('Second', ownedReady('b')),
    ]);

    expect(split.playable.map((p) => p.owned.trackId)).toEqual(['a', 'b']);
  });
});

describe('toPlaybackQueue', () => {
  it('maps playables onto library playback tracks', () => {
    const split = splitOwned([trackResult('Song', ownedReady('track-9'))]);

    const queue = toPlaybackQueue(split.playable, 'Fallback Artist', 'art.png');

    expect(queue).toEqual([
      {
        source: { kind: 'library', trackId: 'track-9' },
        title: 'Song',
        artist: 'Artist',
        artworkUrl: 'art.png',
        durationSeconds: undefined,
      },
    ]);
  });
});

describe('playButtonState', () => {
  it('disables Play when nothing is playable', () => {
    expect(playButtonState({ playable: [], unownedCount: 3, acquiringCount: 0 })).toEqual({
      label: 'Play',
      disabled: true,
    });
  });

  it('names the count when the list is only partly playable', () => {
    const split = splitOwned([
      trackResult('One', ownedReady('a')),
      trackResult('Two'),
    ]);

    expect(playButtonState(split)).toEqual({ label: 'Play 1', disabled: false });
  });

  it('stays a bare Play when everything is playable', () => {
    const split = splitOwned([trackResult('One', ownedReady('a'))]);

    expect(playButtonState(split)).toEqual({ label: 'Play', disabled: false });
  });
});
