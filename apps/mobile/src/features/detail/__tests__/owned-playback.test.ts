import type { AcquisitionStatus } from '@shared/api-client/types';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import { canPlay } from '@shared/playback/canPlay';

import { playButtonState, splitOwned, toPlaybackQueue } from '../owned-playback';

type ResultOverrides = {
  title?: string;
  subtitle?: string | null;
  image_url?: string | null;
  extras?: Record<string, unknown>;
};

function trackResult(overrides: ResultOverrides = {}): DiscoveryResult {
  return {
    kind: 'track',
    title: overrides.title ?? 'Title',
    subtitle: overrides.subtitle === undefined ? 'Artist' : overrides.subtitle,
    image_url: overrides.image_url === undefined ? 'https://cdn/img.jpg' : overrides.image_url,
    confidence: 'high',
    sources: [],
    extras: overrides.extras ?? {},
  };
}

function ownedExtras(status: AcquisitionStatus, trackId = 'track-a'): Record<string, unknown> {
  return { owned_track_id: trackId, owned_acquisition_status: status };
}

describe('splitOwned', () => {
  it('counts a result with no ownership stamp as unowned', () => {
    const split = splitOwned([trackResult({ extras: {} })]);

    expect(split.unownedCount).toBe(1);
    expect(split.acquiringCount).toBe(0);
    expect(split.playable).toEqual([]);
  });

  it('routes a ready-owned track into the playable set', () => {
    const result = trackResult({ extras: ownedExtras('ready') });

    const split = splitOwned([result]);

    expect(split.playable).toEqual([
      { owned: { trackId: 'track-a', acquisitionStatus: 'ready' }, result },
    ]);
    expect(split.unownedCount).toBe(0);
    expect(split.acquiringCount).toBe(0);
  });

  it('counts a pending-owned track as acquiring, not playable', () => {
    const split = splitOwned([trackResult({ extras: ownedExtras('pending') })]);

    expect(split.acquiringCount).toBe(1);
    expect(split.playable).toEqual([]);
  });

  it('counts a failed-owned track as acquiring, not playable', () => {
    const split = splitOwned([trackResult({ extras: ownedExtras('failed') })]);

    expect(split.acquiringCount).toBe(1);
    expect(split.playable).toEqual([]);
  });

  it('accumulates each bucket independently across a mixed list', () => {
    const split = splitOwned([
      trackResult({ extras: ownedExtras('ready', 'r1') }),
      trackResult({ extras: ownedExtras('ready', 'r2') }),
      trackResult({ extras: ownedExtras('pending', 'p1') }),
      trackResult({ extras: {} }),
    ]);

    expect(split.playable.map((p) => p.owned.trackId)).toEqual(['r1', 'r2']);
    expect(split.acquiringCount).toBe(1);
    expect(split.unownedCount).toBe(1);
  });

  it('admits a track to playback exactly when the shared playability rule admits its status', () => {
    const statuses: AcquisitionStatus[] = ['ready', 'pending', 'failed'];

    for (const status of statuses) {
      const split = splitOwned([trackResult({ extras: ownedExtras(status) })]);
      const isPlayable = split.playable.length === 1;

      expect(isPlayable).toBe(canPlay(status));
    }
  });
});

describe('toPlaybackQueue', () => {
  it('maps an owned track to a library source carrying its track id', () => {
    const result = trackResult({ extras: ownedExtras('ready') });
    const [entry] = toPlaybackQueue(
      [{ owned: { trackId: 'track-a', acquisitionStatus: 'ready' }, result }],
      null,
      null,
    );

    expect(entry?.source).toEqual({ kind: 'library', trackId: 'track-a' });
  });

  it('prefers the result subtitle over the fallback artist', () => {
    const result = trackResult({ subtitle: 'Real Artist' });
    const [entry] = toPlaybackQueue(
      [{ owned: { trackId: 't', acquisitionStatus: 'ready' }, result }],
      'Fallback Artist',
      null,
    );

    expect(entry?.artist).toBe('Real Artist');
  });

  it('falls back to the fallback artist when the subtitle is absent', () => {
    const result = trackResult({ subtitle: null });
    const [entry] = toPlaybackQueue(
      [{ owned: { trackId: 't', acquisitionStatus: 'ready' }, result }],
      'Fallback Artist',
      null,
    );

    expect(entry?.artist).toBe('Fallback Artist');
  });

  it('falls back to an empty artist when both subtitle and fallback are absent', () => {
    const result = trackResult({ subtitle: null });
    const [entry] = toPlaybackQueue(
      [{ owned: { trackId: 't', acquisitionStatus: 'ready' }, result }],
      null,
      null,
    );

    expect(entry?.artist).toBe('');
  });

  it('falls back to the fallback artwork when the result has none', () => {
    const result = trackResult({ image_url: null });
    const [entry] = toPlaybackQueue(
      [{ owned: { trackId: 't', acquisitionStatus: 'ready' }, result }],
      null,
      'https://cdn/fallback.jpg',
    );

    expect(entry?.artworkUrl).toBe('https://cdn/fallback.jpg');
  });

  it('carries the result artwork over the fallback when present', () => {
    const result = trackResult({ image_url: 'https://cdn/own.jpg' });
    const [entry] = toPlaybackQueue(
      [{ owned: { trackId: 't', acquisitionStatus: 'ready' }, result }],
      null,
      'https://cdn/fallback.jpg',
    );

    expect(entry?.artworkUrl).toBe('https://cdn/own.jpg');
  });

  it('passes the parsed duration through and leaves it undefined when absent', () => {
    const withDuration = trackResult({ extras: { duration_seconds: 200 } });
    const withoutDuration = trackResult({ extras: {} });

    const [a, b] = toPlaybackQueue(
      [
        { owned: { trackId: 'a', acquisitionStatus: 'ready' }, result: withDuration },
        { owned: { trackId: 'b', acquisitionStatus: 'ready' }, result: withoutDuration },
      ],
      null,
      null,
    );

    expect(a?.durationSeconds).toBe(200);
    expect(b?.durationSeconds).toBeUndefined();
  });
});

describe('playButtonState', () => {
  it('disables with the bare Play label when nothing is playable', () => {
    expect(playButtonState({ playable: [], unownedCount: 3, acquiringCount: 1 })).toEqual({
      label: 'Play',
      disabled: true,
    });
  });

  it('labels with the playable count while some tracks are still unowned', () => {
    const split = {
      playable: [
        { owned: { trackId: 'a', acquisitionStatus: 'ready' as const }, result: trackResult() },
        { owned: { trackId: 'b', acquisitionStatus: 'ready' as const }, result: trackResult() },
      ],
      unownedCount: 1,
      acquiringCount: 0,
    };

    expect(playButtonState(split)).toEqual({ label: 'Play 2', disabled: false });
  });

  it('labels with the playable count while some tracks are still acquiring', () => {
    const split = {
      playable: [
        { owned: { trackId: 'a', acquisitionStatus: 'ready' as const }, result: trackResult() },
      ],
      unownedCount: 0,
      acquiringCount: 2,
    };

    expect(playButtonState(split)).toEqual({ label: 'Play 1', disabled: false });
  });

  it('uses the bare Play label when every track is playable', () => {
    const split = {
      playable: [
        { owned: { trackId: 'a', acquisitionStatus: 'ready' as const }, result: trackResult() },
      ],
      unownedCount: 0,
      acquiringCount: 0,
    };

    expect(playButtonState(split)).toEqual({ label: 'Play', disabled: false });
  });
});
