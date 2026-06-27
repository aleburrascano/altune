import { trackExtras, albumExtras } from '../extras-accessors';

describe('trackExtras', () => {
  it('extracts all fields when present and valid', () => {
    const result = trackExtras({
      duration_seconds: 240,
      album: 'OK Computer',
      isrc: 'GBAYE9700123',
      year: 1997,
      genre: 'Alternative',
      album_artist: 'Radiohead',
      featured_artists: ['Thom Yorke', 'Jonny Greenwood'],
      track_id: 'abc-123',
      acquisition_status: 'ready',
      preview_url: 'https://preview.mp3',
      mbid: 'mb-id-1',
    });

    expect(result.durationSeconds).toBe(240);
    expect(result.album).toBe('OK Computer');
    expect(result.isrc).toBe('GBAYE9700123');
    expect(result.year).toBe(1997);
    expect(result.genre).toBe('Alternative');
    expect(result.albumArtist).toBe('Radiohead');
    expect(result.featuredArtists).toEqual(['Thom Yorke', 'Jonny Greenwood']);
    expect(result.trackId).toBe('abc-123');
    expect(result.acquisitionStatus).toBe('ready');
    expect(result.previewUrl).toBe('https://preview.mp3');
    expect(result.mbid).toBe('mb-id-1');
  });

  it('returns null for missing fields', () => {
    const result = trackExtras({});
    expect(result.durationSeconds).toBeNull();
    expect(result.album).toBeNull();
    expect(result.isrc).toBeNull();
    expect(result.year).toBeNull();
    expect(result.genre).toBeNull();
    expect(result.albumArtist).toBeNull();
    expect(result.featuredArtists).toEqual([]);
    expect(result.trackId).toBeNull();
    expect(result.acquisitionStatus).toBeNull();
    expect(result.previewUrl).toBeNull();
    expect(result.mbid).toBeNull();
  });

  it('treats empty strings as null', () => {
    const result = trackExtras({
      album: '',
      isrc: '',
      genre: '',
      album_artist: '',
      preview_url: '',
    });
    expect(result.album).toBeNull();
    expect(result.isrc).toBeNull();
    expect(result.genre).toBeNull();
    expect(result.albumArtist).toBeNull();
    expect(result.previewUrl).toBeNull();
  });

  it('reads duration from the provider `duration` key', () => {
    expect(trackExtras({ duration: 240 }).durationSeconds).toBe(240);
  });

  it('falls back to legacy `duration_seconds` when `duration` is absent', () => {
    expect(trackExtras({ duration_seconds: 199 }).durationSeconds).toBe(199);
  });

  it('rejects non-finite duration', () => {
    expect(trackExtras({ duration_seconds: Infinity }).durationSeconds).toBeNull();
    expect(trackExtras({ duration_seconds: NaN }).durationSeconds).toBeNull();
  });

  it('filters non-string items from featured_artists', () => {
    const result = trackExtras({ featured_artists: ['Valid', 42, '', null] });
    expect(result.featuredArtists).toEqual(['Valid']);
  });

  it('rejects wrong types gracefully', () => {
    const result = trackExtras({
      duration_seconds: 'not a number',
      album: 42,
      year: 'not a number',
      track_id: 123,
    });
    expect(result.durationSeconds).toBeNull();
    expect(result.album).toBeNull();
    expect(result.year).toBeNull();
    expect(result.trackId).toBeNull();
  });
});

describe('albumExtras', () => {
  it('extracts all fields when present', () => {
    const result = albumExtras({
      release_date: '2024-03-15',
      year: 2024,
      track_count: 12,
      record_type: 'album',
    });
    expect(result.releaseDate).toBe('2024-03-15');
    expect(result.year).toBe('2024');
    expect(result.trackCount).toBe(12);
    expect(result.recordType).toBe('album');
  });

  it('returns null for missing fields', () => {
    const result = albumExtras({});
    expect(result.releaseDate).toBeNull();
    expect(result.year).toBeNull();
    expect(result.trackCount).toBeNull();
    expect(result.recordType).toBeNull();
  });

  it('handles string year', () => {
    expect(albumExtras({ year: '2020' }).year).toBe('2020');
  });

  it('handles numeric year by converting to string', () => {
    expect(albumExtras({ year: 2020 }).year).toBe('2020');
  });
});
