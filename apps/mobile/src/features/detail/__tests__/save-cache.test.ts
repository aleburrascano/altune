import type { CreateTrackRequest } from '@shared/api-client/types';
import type { DiscoveryResult } from '@shared/api-client/discovery';

import { optimisticTrack, toCreateTrackRequest } from '../save-cache';

type ResultOverrides = {
  title?: string;
  subtitle?: string | null;
  image_url?: string | null;
  sources?: DiscoveryResult['sources'];
  extras?: Record<string, unknown>;
};

function result(overrides: ResultOverrides = {}): DiscoveryResult {
  return {
    kind: 'track',
    title: overrides.title ?? 'Song',
    subtitle: overrides.subtitle === undefined ? 'Artist' : overrides.subtitle,
    image_url: overrides.image_url === undefined ? 'https://cdn/art.jpg' : overrides.image_url,
    confidence: 'high',
    sources: overrides.sources ?? [],
    extras: overrides.extras ?? {},
  };
}

describe('toCreateTrackRequest', () => {
  it('maps a result into the create request the tracks API accepts', () => {
    const req = toCreateTrackRequest(
      result({
        extras: {
          album: 'Album',
          duration_seconds: 210.9,
          isrc: 'US-ABC',
          year: 1994,
          genre: 'Rock',
          album_artist: 'Album Artist',
          track_position: 3,
        },
      }),
    );

    const expected: CreateTrackRequest = {
      title: 'Song',
      artist: 'Artist',
      album: 'Album',
      duration_seconds: 210,
      artwork_url: 'https://cdn/art.jpg',
      isrc: 'US-ABC',
      year: 1994,
      genre: 'Rock',
      album_artist: 'Album Artist',
      track_number: 3,
      source_url: null,
    };
    expect(req).toEqual(expected);
  });

  it('floors a fractional duration to whole seconds', () => {
    const req = toCreateTrackRequest(result({ extras: { duration_seconds: 210.9 } }));

    expect(req.duration_seconds).toBe(210);
  });

  it('leaves an absent duration null rather than flooring null', () => {
    expect(toCreateTrackRequest(result({ extras: {} })).duration_seconds).toBeNull();
  });

  it('falls back to an empty artist for a null subtitle', () => {
    expect(toCreateTrackRequest(result({ subtitle: null })).artist).toBe('');
  });

  it('carries the soundcloud source url and omits others', () => {
    const req = toCreateTrackRequest(
      result({
        sources: [
          { provider: 'spotify', external_id: 's1', url: 'https://spotify/x' },
          { provider: 'soundcloud', external_id: 'sc1', url: 'https://sc/x' },
        ],
      }),
    );

    expect(req.source_url).toBe('https://sc/x');
  });

  it('leaves source url null when no soundcloud source is present', () => {
    const req = toCreateTrackRequest(
      result({ sources: [{ provider: 'spotify', external_id: 's1', url: 'https://spotify/x' }] }),
    );

    expect(req.source_url).toBeNull();
  });

  it('includes featured_artists only when present', () => {
    const withFeatured = toCreateTrackRequest(
      result({ extras: { featured_artists: [{ name: 'Guest', mbid: null, deezer_id: null }] } }),
    );
    const withoutFeatured = toCreateTrackRequest(result({ extras: {} }));

    expect(withFeatured.featured_artists).toEqual([{ name: 'Guest', mbid: null, deezer_id: null }]);
    expect('featured_artists' in withoutFeatured).toBe(false);
  });
});

describe('optimisticTrack', () => {
  it('stamps a pending placeholder keyed by title and artist', () => {
    const body = toCreateTrackRequest(result());

    const track = optimisticTrack(body, '2026-01-01T00:00:00Z');

    expect(track.id).toBe('optimistic:SongArtist');
    expect(track.acquisition_status).toBe('pending');
    expect(track.added_at).toBe('2026-01-01T00:00:00Z');
    expect(track.audio_ref).toBeNull();
  });

  it('carries every populated request field onto the placeholder', () => {
    const body = toCreateTrackRequest(
      result({
        extras: {
          year: 1994,
          genre: 'Rock',
          track_position: 3,
          album_artist: 'Album Artist',
          isrc: 'US-ABC',
          featured_artists: [{ name: 'Guest', mbid: null, deezer_id: null }],
        },
      }),
    );

    const track = optimisticTrack(body, '2026-01-01T00:00:00Z');

    expect(track.year).toBe(1994);
    expect(track.genre).toBe('Rock');
    expect(track.track_number).toBe(3);
    expect(track.album_artist).toBe('Album Artist');
    expect(track.isrc).toBe('US-ABC');
    expect(track.featured_artists).toEqual([{ name: 'Guest', mbid: null, deezer_id: null }]);
  });

  it('leaves featured_artists off the placeholder when the request omits them', () => {
    const track = optimisticTrack(toCreateTrackRequest(result({ extras: {} })), '2026-01-01T00:00:00Z');

    expect('featured_artists' in track).toBe(false);
  });
});
