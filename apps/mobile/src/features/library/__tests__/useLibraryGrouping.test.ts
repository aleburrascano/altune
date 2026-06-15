import type { TrackResponse } from '@shared/api-client/types';

import { deriveAlbums, deriveArtists } from '../hooks/useLibraryGrouping';

function track(overrides: Partial<TrackResponse> = {}): TrackResponse {
  return {
    id: '00000000-0000-0000-0000-000000000001',
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

describe('deriveAlbums', () => {
  it('returns empty for tracks with no album', () => {
    const result = deriveAlbums([track(), track()]);
    expect(result).toEqual([]);
  });

  it('groups tracks by album + artist', () => {
    const result = deriveAlbums([
      track({ album: 'Album A', artist: 'Band' }),
      track({ album: 'Album A', artist: 'Band', id: '2' }),
    ]);
    expect(result).toHaveLength(1);
    expect(result[0]!.album).toBe('Album A');
    expect(result[0]!.trackCount).toBe(2);
  });

  it('uses album_artist over artist when present', () => {
    const result = deriveAlbums([
      track({ album: 'Collab', artist: 'Feat Artist', album_artist: 'Main Artist' }),
    ]);
    expect(result[0]!.artist).toBe('Main Artist');
  });

  it('treats different-case album names as the same group', () => {
    const result = deriveAlbums([
      track({ album: 'My Album', artist: 'Band' }),
      track({ album: 'my album', artist: 'band', id: '2' }),
    ]);
    expect(result).toHaveLength(1);
    expect(result[0]!.trackCount).toBe(2);
  });

  it('picks the most recent added_at', () => {
    const result = deriveAlbums([
      track({ album: 'A', artist: 'B', added_at: '2026-01-01T00:00:00Z' }),
      track({ album: 'A', artist: 'B', added_at: '2026-06-15T00:00:00Z', id: '2' }),
    ]);
    expect(result[0]!.mostRecentAddedAt).toBe('2026-06-15T00:00:00Z');
  });

  it('uses artwork from first track, falls back for later tracks', () => {
    const result = deriveAlbums([
      track({ album: 'A', artist: 'B', artwork_url: null }),
      track({ album: 'A', artist: 'B', artwork_url: 'https://img.jpg', id: '2' }),
    ]);
    expect(result[0]!.artworkUrl).toBe('https://img.jpg');
  });

  it('skips tracks with empty string album', () => {
    const result = deriveAlbums([track({ album: '' })]);
    expect(result).toEqual([]);
  });
});

describe('deriveArtists', () => {
  it('returns empty for empty input', () => {
    expect(deriveArtists([])).toEqual([]);
  });

  it('groups tracks by case-insensitive artist name', () => {
    const result = deriveArtists([
      track({ artist: 'Radiohead' }),
      track({ artist: 'radiohead', id: '2' }),
    ]);
    expect(result).toHaveLength(1);
    expect(result[0]!.trackCount).toBe(2);
  });

  it('preserves original artist casing from first occurrence', () => {
    const result = deriveArtists([
      track({ artist: 'Radiohead' }),
      track({ artist: 'RADIOHEAD', id: '2' }),
    ]);
    expect(result[0]!.artist).toBe('Radiohead');
  });

  it('picks the most recent added_at', () => {
    const result = deriveArtists([
      track({ artist: 'A', added_at: '2026-01-01T00:00:00Z' }),
      track({ artist: 'A', added_at: '2026-06-15T00:00:00Z', id: '2' }),
    ]);
    expect(result[0]!.mostRecentAddedAt).toBe('2026-06-15T00:00:00Z');
  });

  it('falls back to first available artwork', () => {
    const result = deriveArtists([
      track({ artist: 'A', artwork_url: null }),
      track({ artist: 'A', artwork_url: 'https://art.jpg', id: '2' }),
    ]);
    expect(result[0]!.artworkUrl).toBe('https://art.jpg');
  });
});
