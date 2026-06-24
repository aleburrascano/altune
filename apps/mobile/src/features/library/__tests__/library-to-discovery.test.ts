import type { AlbumGroup, ArtistGroup } from '../hooks/useLibraryGrouping';
import { albumToDiscoveryResult, artistToDiscoveryResult } from '../ui/library-to-discovery';

const baseAlbum: AlbumGroup = {
  key: 'rumours|||fleetwood mac',
  album: 'Rumours',
  artist: 'Fleetwood Mac',
  artworkUrl: 'https://img/rumours.jpg',
  year: 1977,
  trackCount: 11,
  mostRecentAddedAt: '2026-06-01T00:00:00Z',
};

const baseArtist: ArtistGroup = {
  key: 'fleetwood mac',
  artist: 'Fleetwood Mac',
  artworkUrl: 'https://img/fm.jpg',
  trackCount: 42,
  mostRecentAddedAt: '2026-06-01T00:00:00Z',
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
    expect(albumToDiscoveryResult({ ...baseAlbum, artworkUrl: null }).image_url).toBeNull();
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
    expect(artistToDiscoveryResult({ ...baseArtist, artworkUrl: null }).image_url).toBeNull();
  });
});
