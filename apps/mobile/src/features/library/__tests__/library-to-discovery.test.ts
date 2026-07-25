import type { AlbumGroup, ArtistGroup } from '@shared/api-client/library';
import { albumToDiscoveryResult, artistToDiscoveryResult } from '../ui/library-to-discovery';

const baseAlbum: AlbumGroup = {
  key: 'rumours|||fleetwood mac',
  album: 'Rumours',
  artist: 'Fleetwood Mac',
  artwork_url: 'https://img/rumours.jpg',
  year: 1977,
  track_count: 11,
  most_recent_added_at: '2026-06-01T00:00:00Z',
};

const baseArtist: ArtistGroup = {
  key: 'fleetwood mac',
  artist: 'Fleetwood Mac',
  artwork_url: 'https://img/fm.jpg',
  track_count: 42,
  most_recent_added_at: '2026-06-01T00:00:00Z',
};

describe('albumToDiscoveryResult', () => {
  it('maps an album group to an album discovery result', () => {
    expect(albumToDiscoveryResult(baseAlbum)).toEqual({
      kind: 'album',
      title: 'Rumours',
      subtitle: 'Fleetwood Mac',
      image_url: 'https://img/rumours.jpg',
      confidence: 'high',
      sources: [],
      extras: { year: 1977, track_count: 11 },
    });
  });

  it('omits year from extras when the album has none', () => {
    const result = albumToDiscoveryResult({ ...baseAlbum, year: null });
    expect(result.extras).toEqual({ track_count: 11 });
    expect('year' in result.extras).toBe(false);
  });

  it('passes a null artwork url straight through', () => {
    expect(albumToDiscoveryResult({ ...baseAlbum, artwork_url: null }).image_url).toBeNull();
  });
});

describe('artistToDiscoveryResult', () => {
  it('maps an artist group to an artist discovery result with no subtitle', () => {
    expect(artistToDiscoveryResult(baseArtist)).toEqual({
      kind: 'artist',
      title: 'Fleetwood Mac',
      subtitle: null,
      image_url: 'https://img/fm.jpg',
      confidence: 'high',
      sources: [],
      extras: {},
    });
  });

  it('passes a null artwork url straight through', () => {
    expect(artistToDiscoveryResult({ ...baseArtist, artwork_url: null }).image_url).toBeNull();
  });
});
