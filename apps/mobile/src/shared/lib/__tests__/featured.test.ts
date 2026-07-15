import { featuredArtistsFromExtras, withFeaturing } from '../featured';

describe('withFeaturing', () => {
  it('comma-joins guests into the base artist', () => {
    expect(
      withFeaturing('Ken Carson', [
        { name: 'Playboi Carti', mbid: null, deezer_id: null },
        { name: 'Destroy Lonely', mbid: null, deezer_id: null },
      ]),
    ).toBe('Ken Carson, Playboi Carti, Destroy Lonely');
  });
  it('returns base when absent', () => {
    expect(withFeaturing('Drake', [])).toBe('Drake');
    expect(withFeaturing('Drake', undefined)).toBe('Drake');
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
