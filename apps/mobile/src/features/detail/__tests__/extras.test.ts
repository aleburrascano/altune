/**
 * extractFeaturedFromText / resolveFeatured — the track body's three-tier
 * featured-artist fallback: structured extras → Deezer enrichment → regex
 * parse of "feat./ft./with" in title/subtitle.
 */

import { extractFeaturedFromText, resolveFeatured } from '../extras';

describe('extractFeaturedFromText', () => {
  it('parses feat./ft./featuring/with markers from the title', () => {
    expect(extractFeaturedFromText('Song (feat. Artist A)', null)).toBe('Artist A');
    expect(extractFeaturedFromText('Song ft. Artist B', null)).toBe('Artist B');
    expect(extractFeaturedFromText('Song featuring Artist C', null)).toBe('Artist C');
    expect(extractFeaturedFromText('Song [with Artist D]', null)).toBe('Artist D');
  });

  it('falls back to the subtitle when the title has no marker', () => {
    expect(extractFeaturedFromText('Song', 'Main Artist feat. Guest')).toBe('Guest');
  });

  it('returns null when neither carries a marker', () => {
    expect(extractFeaturedFromText('Song', 'Main Artist')).toBeNull();
    expect(extractFeaturedFromText('Song', null)).toBeNull();
  });
});

describe('resolveFeatured', () => {
  const structured = [{ name: 'Structured Guest', mbid: 'mb-1', deezer_id: 5 }];
  const deezer = [{ name: 'Deezer Guest', mbid: null, deezer_id: 9 }];

  it('prefers structured extras.featured_artists over everything', () => {
    const featured = resolveFeatured(
      { featured_artists: structured },
      deezer,
      'Song (feat. Text Guest)',
      'Main',
    );
    expect(featured).toEqual(structured);
  });

  it('falls back to Deezer enrichment contributors when extras carry none', () => {
    expect(resolveFeatured({}, deezer, 'Song (feat. Text Guest)', 'Main')).toEqual(deezer);
  });

  it('falls back to regex-parsed bare names last', () => {
    expect(resolveFeatured({}, undefined, 'Song (feat. Guest A, Guest B)', 'Main')).toEqual([
      { name: 'Guest A', mbid: null, deezer_id: null },
      { name: 'Guest B', mbid: null, deezer_id: null },
    ]);
    expect(resolveFeatured({}, [], 'Song (feat. Guest A)', 'Main')).toEqual([
      { name: 'Guest A', mbid: null, deezer_id: null },
    ]);
  });

  it('resolves to empty when no tier matches', () => {
    expect(resolveFeatured({}, undefined, 'Song', 'Main')).toEqual([]);
  });
});
