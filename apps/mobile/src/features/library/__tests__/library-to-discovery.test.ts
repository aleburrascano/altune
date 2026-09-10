import type { AlbumGroup, ArtistGroup } from '@shared/api-client/library';

import { albumToDiscoveryResult, artistToDiscoveryResult } from '../ui/library-to-discovery';

function makeAlbum(over: Partial<AlbumGroup> = {}): AlbumGroup {
  return {
    key: 'alb-1',
    album: 'Discovery',
    artist: 'Daft Punk',
    artwork_url: 'https://a.example/discovery.jpg',
    year: 2001,
    track_count: 14,
    most_recent_added_at: '2026-01-01T00:00:00Z',
    ...over,
  };
}

function makeArtist(over: Partial<ArtistGroup> = {}): ArtistGroup {
  return {
    key: 'art-1',
    artist: 'Daft Punk',
    artwork_url: 'https://a.example/dp.jpg',
    track_count: 27,
    most_recent_added_at: '2026-01-01T00:00:00Z',
    ...over,
  };
}

describe('albumToDiscoveryResult — server-grouped album row to a discovery card', () => {
  it('maps the fixed fields from the server group without re-deriving them', () => {
    const result = albumToDiscoveryResult(makeAlbum());
    expect(result.kind).toBe('album');
    expect(result.title).toBe('Discovery');
    expect(result.subtitle).toBe('Daft Punk');
    expect(result.image_url).toBe('https://a.example/discovery.jpg');
    expect(result.confidence).toBe('high');
    expect(result.sources).toEqual([]);
  });

  it('always carries track_count in extras', () => {
    expect(albumToDiscoveryResult(makeAlbum({ track_count: 14 })).extras['track_count']).toBe(14);
  });

  it('omits year from extras when the server sent null', () => {
    expect(albumToDiscoveryResult(makeAlbum({ year: null })).extras).not.toHaveProperty('year');
  });

  it('includes a year of exactly 0, the falsy-but-valid boundary the != null guard preserves', () => {
    expect(albumToDiscoveryResult(makeAlbum({ year: 0 })).extras['year']).toBe(0);
  });

  it('includes a real year alongside track_count', () => {
    const extras = albumToDiscoveryResult(makeAlbum({ year: 2001, track_count: 14 })).extras;
    expect(extras['year']).toBe(2001);
    expect(extras['track_count']).toBe(14);
  });

  it('passes a null artwork through as a null image_url rather than an empty string', () => {
    expect(albumToDiscoveryResult(makeAlbum({ artwork_url: null })).image_url).toBeNull();
  });
});

describe('artistToDiscoveryResult — server-grouped artist row to a discovery card', () => {
  it('maps kind, title and a null subtitle, and leaves extras empty', () => {
    const result = artistToDiscoveryResult(makeArtist());
    expect(result.kind).toBe('artist');
    expect(result.title).toBe('Daft Punk');
    expect(result.subtitle).toBeNull();
    expect(result.confidence).toBe('high');
    expect(result.sources).toEqual([]);
    expect(result.extras).toEqual({});
  });

  it('passes the artist artwork through as image_url', () => {
    expect(artistToDiscoveryResult(makeArtist({ artwork_url: 'https://a.example/x.jpg' })).image_url).toBe(
      'https://a.example/x.jpg',
    );
  });

  it('never borrows track_count into an artist card, unlike an album card', () => {
    expect(artistToDiscoveryResult(makeArtist({ track_count: 27 })).extras).not.toHaveProperty(
      'track_count',
    );
  });
});
