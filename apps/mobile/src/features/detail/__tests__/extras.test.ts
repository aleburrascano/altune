import type { FeaturedArtist } from '@shared/api-client/types';

import { extractFeaturedFromText, resolveFeatured } from '../extras';

function featured(name: string): FeaturedArtist {
  return { name, mbid: null, deezer_id: null };
}

describe('extractFeaturedFromText', () => {
  it('reads a parenthesised feat. credit from the title', () => {
    expect(extractFeaturedFromText('Song (feat. Guest)', null)).toBe('Guest');
  });

  it('reads a bracketed ft credit', () => {
    expect(extractFeaturedFromText('Song [ft Guest]', null)).toBe('Guest');
  });

  it('reads a feat credit written without the trailing dot', () => {
    expect(extractFeaturedFromText('Song (feat Guest)', null)).toBe('Guest');
  });

  it('trims whitespace around the captured credit', () => {
    expect(extractFeaturedFromText('Song (feat. Guest )', null)).toBe('Guest');
  });

  it('reads a with credit that runs to the end of the string', () => {
    expect(extractFeaturedFromText('Song with Guest', null)).toBe('Guest');
  });

  it('falls through to the subtitle when the title has no credit', () => {
    expect(extractFeaturedFromText('Song', 'Artist featuring Guest')).toBe('Guest');
  });

  it('returns null when neither field carries a credit', () => {
    expect(extractFeaturedFromText('Song', 'Artist')).toBeNull();
  });

  it('returns null for a null subtitle with no credit in the title', () => {
    expect(extractFeaturedFromText('Song', null)).toBeNull();
  });
});

describe('resolveFeatured', () => {
  it('prefers structured extras over every other source', () => {
    const result = resolveFeatured(
      { featured_artists: [{ name: 'Structured', mbid: 'mb', deezer_id: 7 }] },
      [featured('Deezer')],
      'Song (feat. Textual)',
      null,
    );

    expect(result).toEqual([{ name: 'Structured', mbid: 'mb', deezer_id: 7 }]);
  });

  it('falls back to Deezer credits when structured extras are empty', () => {
    const result = resolveFeatured({}, [featured('Deezer')], 'Song (feat. Textual)', null);

    expect(result).toEqual([featured('Deezer')]);
  });

  it('parses the title text only when neither structured nor Deezer credits exist', () => {
    const result = resolveFeatured({}, undefined, 'Song (feat. A, B)', null);

    expect(result).toEqual([featured('A'), featured('B')]);
  });

  it('resolves to an empty list when no source yields a credit', () => {
    expect(resolveFeatured({}, [], 'Song', 'Artist')).toEqual([]);
  });

  it('treats an empty Deezer list as absent and reads the text instead', () => {
    const result = resolveFeatured({}, [], 'Song feat. Textual', null);

    expect(result).toEqual([featured('Textual')]);
  });
});
