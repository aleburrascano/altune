import { albumExtras, trackExtras } from '../extras-accessors';

describe('trackExtras', () => {
  it('parses a full wire map into the narrowed track shape', () => {
    const te = trackExtras({
      duration: 210,
      album: 'Album',
      isrc: 'US-ABC-12-34567',
      year: 1994,
      genre: 'Rock',
      album_artist: 'Album Artist',
      track_id: 'track-a',
      acquisition_status: 'ready',
      preview_url: 'https://cdn/p.mp3',
      mbid: 'mb-1',
      track_position: 3,
    });

    expect(te).toEqual({
      durationSeconds: 210,
      album: 'Album',
      isrc: 'US-ABC-12-34567',
      year: 1994,
      genre: 'Rock',
      albumArtist: 'Album Artist',
      featuredArtists: [],
      trackId: 'track-a',
      acquisitionStatus: 'ready',
      previewUrl: 'https://cdn/p.mp3',
      mbid: 'mb-1',
      trackPosition: 3,
    });
  });

  it('nulls every optional field when the wire map is empty', () => {
    const te = trackExtras({});

    expect(te).toEqual({
      durationSeconds: null,
      album: null,
      isrc: null,
      year: null,
      genre: null,
      albumArtist: null,
      featuredArtists: [],
      trackId: null,
      acquisitionStatus: null,
      previewUrl: null,
      mbid: null,
      trackPosition: null,
    });
  });

  it('reads duration from the duration_seconds alias when duration is absent', () => {
    expect(trackExtras({ duration_seconds: 180 }).durationSeconds).toBe(180);
  });

  it('prefers the plain duration key over the alias', () => {
    expect(trackExtras({ duration: 200, duration_seconds: 180 }).durationSeconds).toBe(200);
  });

  it('reads the track id from the owned_track_id stamp when track_id is absent', () => {
    expect(trackExtras({ owned_track_id: 'owned-1' }).trackId).toBe('owned-1');
  });

  it('reads the status from the owned_acquisition_status stamp when acquisition_status is absent', () => {
    expect(trackExtras({ owned_acquisition_status: 'pending' }).acquisitionStatus).toBe('pending');
  });

  it('rejects an unrecognised acquisition status as null', () => {
    expect(trackExtras({ acquisition_status: 'archived' }).acquisitionStatus).toBeNull();
  });

  it('rejects a non-finite duration as null', () => {
    expect(trackExtras({ duration: Number.NaN }).durationSeconds).toBeNull();
  });

  it('rejects a non-numeric duration as null', () => {
    expect(trackExtras({ duration: '200' }).durationSeconds).toBeNull();
  });

  it('rejects an empty album string as null', () => {
    expect(trackExtras({ album: '' }).album).toBeNull();
  });

  it('rejects an empty isrc string as null', () => {
    expect(trackExtras({ isrc: '' }).isrc).toBeNull();
  });

  it('rejects an empty genre string as null', () => {
    expect(trackExtras({ genre: '' }).genre).toBeNull();
  });

  it('rejects an empty album_artist string as null', () => {
    expect(trackExtras({ album_artist: '' }).albumArtist).toBeNull();
  });

  it('rejects a non-numeric year as null', () => {
    expect(trackExtras({ year: '1994' }).year).toBeNull();
  });

  it('rejects a non-finite year as null', () => {
    expect(trackExtras({ year: Number.NaN }).year).toBeNull();
  });

  it('rejects a non-numeric track position as null', () => {
    expect(trackExtras({ track_position: '3' }).trackPosition).toBeNull();
  });

  it('rejects a non-finite track position as null', () => {
    expect(trackExtras({ track_position: Number.NaN }).trackPosition).toBeNull();
  });

  it('rejects an empty preview url as null', () => {
    expect(trackExtras({ preview_url: '' }).previewUrl).toBeNull();
  });
});

describe('albumExtras', () => {
  it('keeps a string release date and stringifies a numeric year', () => {
    expect(albumExtras({ release_date: '1994-11-01', year: 1994 })).toEqual({
      releaseDate: '1994-11-01',
      year: '1994',
      trackCount: null,
      recordType: null,
    });
  });

  it('keeps a string year as-is', () => {
    expect(albumExtras({ year: '1994' }).year).toBe('1994');
  });

  it('reads the track count and record type when numeric and string', () => {
    const ae = albumExtras({ track_count: 12, record_type: 'album' });

    expect(ae.trackCount).toBe(12);
    expect(ae.recordType).toBe('album');
  });

  it('nulls every field for an empty map', () => {
    expect(albumExtras({})).toEqual({
      releaseDate: null,
      year: null,
      trackCount: null,
      recordType: null,
    });
  });
});
