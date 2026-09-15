import { resultSecondaryLine } from '../resultSecondaryLine';

import type { DiscoveryResult } from '@shared/api-client/discovery';

function resultFixture(overrides: Partial<DiscoveryResult> = {}): DiscoveryResult {
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
    const line = resultSecondaryLine(resultFixture({ kind: 'artist', subtitle: 'Genre' }));

    expect(line).toBe('Artist');
  });

  it('joins kind, artist and year for an album', () => {
    const line = resultSecondaryLine(
      resultFixture({ kind: 'album', subtitle: 'Band', extras: { year: 1999 } }),
    );

    expect(line).toBe('Album · Band · 1999');
  });

  it('accepts a string year and drops a non-scalar year for an album', () => {
    const stringYear = resultSecondaryLine(
      resultFixture({ kind: 'album', subtitle: null, extras: { year: '2004' } }),
    );
    const objectYear = resultSecondaryLine(
      resultFixture({ kind: 'album', subtitle: 'Band', extras: { year: { v: 1 } } }),
    );

    expect(stringYear).toBe('Album · 2004');
    expect(objectYear).toBe('Album · Band');
  });

  it('appends featured guests to a track artist', () => {
    const line = resultSecondaryLine(
      resultFixture({
        subtitle: 'Lead',
        extras: { featured_artists: ['Guest A', { name: 'Guest B' }] },
      }),
    );

    expect(line).toBe('Track · Lead, Guest A, Guest B');
  });

  it('omits the artist segment for a track without a subtitle', () => {
    const line = resultSecondaryLine(resultFixture({ extras: { featured_artists: ['Guest'] } }));

    expect(line).toBe('Track');
  });

  it('counts extra versions beyond the first, pluralising above one', () => {
    const single = resultSecondaryLine(resultFixture({ extras: { variant_count: 2 } }));
    const plural = resultSecondaryLine(resultFixture({ extras: { variant_count: 4 } }));
    const none = resultSecondaryLine(resultFixture({ extras: { variant_count: 1 } }));

    expect(single).toBe('Track · +1 version');
    expect(plural).toBe('Track · +3 versions');
    expect(none).toBe('Track');
  });
});
