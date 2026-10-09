import {
  kindLabel,
  resultKey,
  resultSecondaryLine,
} from '@features/discovery/domain/resultDisplay';
import { resultFixture } from '@features/discovery/__tests__/fixtures';

import type { DiscoveryKind, DiscoveryResult } from '@shared/api-client/discovery';

describe('kindLabel renders the user-facing kind label', () => {
  const cases: [DiscoveryKind, string, string][] = [
    ['artist', 'Artist', 'Artists'],
    ['album', 'Album', 'Albums'],
    ['track', 'Track', 'Tracks'],
  ];

  it.each(cases)('labels %s as singular by default', (kind, singular) => {
    expect(kindLabel(kind)).toBe(singular);
  });

  it.each(cases)('labels %s as singular when plural is explicitly false', (kind, singular) => {
    expect(kindLabel(kind, { plural: false })).toBe(singular);
  });

  it.each(cases)('labels %s as plural when plural is true', (kind, _singular, plural) => {
    expect(kindLabel(kind, { plural: true })).toBe(plural);
  });

  it('never surfaces the banned noun for the track kind', () => {
    expect(kindLabel('track')).not.toMatch(/song/i);
    expect(kindLabel('track', { plural: true })).not.toMatch(/song/i);
  });
});

describe('resultKey builds a stable key with source-aware fallbacks', () => {
  it('uses kind, first provider and external id when a source with an id is present', () => {
    const key = resultKey(
      resultFixture({
        kind: 'album',
        sources: [{ provider: 'tidal', external_id: 'abc', url: 'u' }],
      }),
      3,
    );

    expect(key).toBe('album-tidal-abc');
  });

  it('falls back to title and index when the first source has an empty external id', () => {
    const key = resultKey(
      resultFixture({
        kind: 'track',
        title: 'Karma Police',
        sources: [{ provider: 'spotify', external_id: '', url: 'u' }],
      }),
      2,
    );

    expect(key).toBe('track-spotify-Karma Police-2');
  });

  it('falls back to a placeholder provider and title-index when there is no source', () => {
    const key = resultKey(resultFixture({ kind: 'artist', title: 'Thom Yorke', sources: [] }), 5);

    expect(key).toBe('artist-x-Thom Yorke-5');
  });
});

function lineFixture(overrides: Partial<DiscoveryResult> = {}): DiscoveryResult {
  return {
    kind: 'track',
    title: 'The Title',
    subtitle: null,
    image_url: null,
    confidence: 'high',
    sources: [{ provider: 'spotify', external_id: 'ext-1', url: 'https://x' }],
    extras: {},
    ...overrides,
  };
}

describe('resultSecondaryLine', () => {
  it('shows only the kind for an artist, ignoring its subtitle', () => {
    const line = resultSecondaryLine(lineFixture({ kind: 'artist', subtitle: 'Genre' }));

    expect(line).toBe('Artist');
  });

  it('joins kind, artist and year for an album', () => {
    const line = resultSecondaryLine(
      lineFixture({ kind: 'album', subtitle: 'Band', extras: { year: 1999 } }),
    );

    expect(line).toBe('Album · Band · 1999');
  });

  it('accepts a string year and drops a non-scalar year for an album', () => {
    const stringYear = resultSecondaryLine(
      lineFixture({ kind: 'album', subtitle: null, extras: { year: '2004' } }),
    );
    const objectYear = resultSecondaryLine(
      lineFixture({ kind: 'album', subtitle: 'Band', extras: { year: { v: 1 } } }),
    );

    expect(stringYear).toBe('Album · 2004');
    expect(objectYear).toBe('Album · Band');
  });

  it('appends featured guests to a track artist', () => {
    const line = resultSecondaryLine(
      lineFixture({
        subtitle: 'Lead',
        extras: { featured_artists: ['Guest A', { name: 'Guest B' }] },
      }),
    );

    expect(line).toBe('Track · Lead, Guest A, Guest B');
  });

  it('omits the artist segment for a track without a subtitle', () => {
    const line = resultSecondaryLine(lineFixture({ extras: { featured_artists: ['Guest'] } }));

    expect(line).toBe('Track');
  });

  it('counts extra versions beyond the first, pluralising above one', () => {
    const single = resultSecondaryLine(lineFixture({ extras: { variant_count: 2 } }));
    const plural = resultSecondaryLine(lineFixture({ extras: { variant_count: 4 } }));
    const none = resultSecondaryLine(lineFixture({ extras: { variant_count: 1 } }));

    expect(single).toBe('Track · +1 version');
    expect(plural).toBe('Track · +3 versions');
    expect(none).toBe('Track');
  });
});
