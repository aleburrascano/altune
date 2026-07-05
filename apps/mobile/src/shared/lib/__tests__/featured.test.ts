import { featuredArtistsFromExtras, formatFeaturing, withFeaturing } from '../featured';

describe('formatFeaturing', () => {
  it('formats a list', () => {
    expect(formatFeaturing([{ name: 'A', mbid: null, deezer_id: null }, { name: 'B', mbid: null, deezer_id: null }])).toBe(
      'feat. A, B',
    );
  });
  it('returns null for empty/undefined', () => {
    expect(formatFeaturing([])).toBeNull();
    expect(formatFeaturing(undefined)).toBeNull();
  });
});

describe('withFeaturing', () => {
  it('appends when present', () => {
    expect(withFeaturing('Drake', [{ name: 'Rihanna', mbid: null, deezer_id: null }])).toBe('Drake · feat. Rihanna');
  });
  it('returns base when absent', () => {
    expect(withFeaturing('Drake', [])).toBe('Drake');
  });
});

describe('featuredArtistsFromExtras', () => {
  it('parses objects, tolerates legacy strings, drops junk', () => {
    expect(
      featuredArtistsFromExtras([
        { name: 'Obj', mbid: 'm', deezer_id: 3 },
        'Legacy',
        { name: '' },
        7,
        null,
      ]),
    ).toEqual([
      { name: 'Obj', mbid: 'm', deezer_id: 3 },
      { name: 'Legacy', mbid: null, deezer_id: null },
    ]);
  });
  it('returns [] for non-array', () => {
    expect(featuredArtistsFromExtras(undefined)).toEqual([]);
    expect(featuredArtistsFromExtras('nope')).toEqual([]);
  });
});
