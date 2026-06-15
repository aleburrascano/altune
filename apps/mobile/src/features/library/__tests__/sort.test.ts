import type { TrackResponse } from '@shared/api-client/types';

import type { AlbumGroup, ArtistGroup } from '../hooks/useLibraryGrouping';
import { sortAlbums, sortArtists, sortTracks } from '../ui/sort';

function albumGroup(overrides: Partial<AlbumGroup> = {}): AlbumGroup {
  return {
    key: 'k',
    album: 'Album',
    artist: 'Artist',
    artworkUrl: null,
    year: null,
    trackCount: 1,
    mostRecentAddedAt: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

function artistGroup(overrides: Partial<ArtistGroup> = {}): ArtistGroup {
  return {
    key: 'k',
    artist: 'Artist',
    artworkUrl: null,
    trackCount: 1,
    mostRecentAddedAt: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

function track(overrides: Partial<TrackResponse> = {}): TrackResponse {
  return {
    id: '1',
    title: 'Track',
    artist: 'Artist',
    album: null,
    duration_seconds: null,
    added_at: '2026-01-01T00:00:00Z',
    acquisition_status: 'pending',
    artwork_url: null,
    failure_reason: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
    ...overrides,
  };
}

describe('sortAlbums', () => {
  it('sorts by most recent added_at descending', () => {
    const albums = [
      albumGroup({ key: 'old', album: 'Old', mostRecentAddedAt: '2025-01-01T00:00:00Z' }),
      albumGroup({ key: 'new', album: 'New', mostRecentAddedAt: '2026-06-01T00:00:00Z' }),
    ];
    const result = sortAlbums(albums, 'recent');
    expect(result[0]!.album).toBe('New');
  });

  it('sorts alphabetically by album name', () => {
    const albums = [
      albumGroup({ key: 'b', album: 'Zeta' }),
      albumGroup({ key: 'a', album: 'Alpha' }),
    ];
    const result = sortAlbums(albums, 'az');
    expect(result[0]!.album).toBe('Alpha');
  });

  it('sorts by year descending, null years last', () => {
    const albums = [
      albumGroup({ key: 'a', album: 'No Year', year: null }),
      albumGroup({ key: 'b', album: 'Old', year: 2020 }),
      albumGroup({ key: 'c', album: 'New', year: 2024 }),
    ];
    const result = sortAlbums(albums, 'year');
    expect(result[0]!.album).toBe('New');
    expect(result[1]!.album).toBe('Old');
    expect(result[2]!.album).toBe('No Year');
  });

  it('returns a new array, does not mutate input', () => {
    const albums = [albumGroup()];
    const result = sortAlbums(albums, 'recent');
    expect(result).not.toBe(albums);
  });
});

describe('sortArtists', () => {
  it('sorts by most recent added_at descending', () => {
    const artists = [
      artistGroup({ key: 'old', artist: 'Old', mostRecentAddedAt: '2025-01-01T00:00:00Z' }),
      artistGroup({ key: 'new', artist: 'New', mostRecentAddedAt: '2026-06-01T00:00:00Z' }),
    ];
    const result = sortArtists(artists, 'recent');
    expect(result[0]!.artist).toBe('New');
  });

  it('sorts alphabetically by artist name', () => {
    const artists = [
      artistGroup({ key: 'z', artist: 'Zeppelin' }),
      artistGroup({ key: 'a', artist: 'ABBA' }),
    ];
    const result = sortArtists(artists, 'az');
    expect(result[0]!.artist).toBe('ABBA');
  });

  it('returns input order for year (no year on artists)', () => {
    const artists = [
      artistGroup({ key: 'a', artist: 'First' }),
      artistGroup({ key: 'b', artist: 'Second' }),
    ];
    const result = sortArtists(artists, 'year');
    expect(result[0]!.artist).toBe('First');
  });

  it('returns a new array, does not mutate input', () => {
    const artists = [artistGroup()];
    const result = sortArtists(artists, 'recent');
    expect(result).not.toBe(artists);
  });
});

describe('sortTracks', () => {
  it('sorts by added_at descending', () => {
    const tracks = [
      track({ title: 'Old', added_at: '2025-01-01T00:00:00Z' }),
      track({ title: 'New', added_at: '2026-06-01T00:00:00Z', id: '2' }),
    ];
    const result = sortTracks(tracks, 'recent');
    expect(result[0]!.title).toBe('New');
  });

  it('sorts alphabetically by title', () => {
    const tracks = [
      track({ title: 'Zebra' }),
      track({ title: 'Alpha', id: '2' }),
    ];
    const result = sortTracks(tracks, 'az');
    expect(result[0]!.title).toBe('Alpha');
  });

  it('sorts by year descending, null years last', () => {
    const tracks = [
      track({ title: 'No Year', year: null }),
      track({ title: 'Old', year: 2018, id: '2' }),
      track({ title: 'New', year: 2024, id: '3' }),
    ];
    const result = sortTracks(tracks, 'year');
    expect(result[0]!.title).toBe('New');
    expect(result[2]!.title).toBe('No Year');
  });

  it('returns a new array, does not mutate input', () => {
    const tracks = [track()];
    const result = sortTracks(tracks, 'recent');
    expect(result).not.toBe(tracks);
  });
});

describe.each(['recent', 'az', 'year'] as const)('sort key %s', (key) => {
  it('sortAlbums handles empty array', () => {
    expect(sortAlbums([], key)).toEqual([]);
  });

  it('sortArtists handles empty array', () => {
    expect(sortArtists([], key)).toEqual([]);
  });

  it('sortTracks handles empty array', () => {
    expect(sortTracks([], key)).toEqual([]);
  });
});
