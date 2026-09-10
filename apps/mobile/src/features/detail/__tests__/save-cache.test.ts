import type {
  CreateTrackRequest,
  ListTracksResponse,
  TrackResponse,
} from '@shared/api-client/types';
import type { DiscoveryResult } from '@shared/api-client/discovery';

import {
  insertOptimisticTrackHome,
  optimisticTrack,
  replaceOptimisticTrackHome,
  toCreateTrackRequest,
} from '../save-cache';

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

function trackResponse(id: string): TrackResponse {
  return {
    id,
    title: 'Song',
    artist: 'Artist',
    album: null,
    duration_seconds: null,
    added_at: '2026-01-01T00:00:00Z',
    acquisition_status: 'ready',
    artwork_url: null,
    failure_reason: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
  };
}

function listResponse(items: TrackResponse[]): ListTracksResponse {
  return { items, total: items.length, limit: 50, offset: 0, has_more: false };
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

describe('insertOptimisticTrackHome', () => {
  it('prepends the placeholder and bumps the total', () => {
    const data = insertOptimisticTrackHome(listResponse([trackResponse('a')]), trackResponse('opt'));

    expect(data?.items.map((t) => t.id)).toEqual(['opt', 'a']);
    expect(data?.total).toBe(2);
  });

  it('leaves undefined data untouched', () => {
    expect(insertOptimisticTrackHome(undefined, trackResponse('opt'))).toBeUndefined();
  });

  it('does not insert a track already present among several existing items', () => {
    const data = insertOptimisticTrackHome(
      listResponse([trackResponse('a'), trackResponse('b')]),
      trackResponse('a'),
    );

    expect(data?.items.map((t) => t.id)).toEqual(['a', 'b']);
    expect(data?.total).toBe(2);
  });
});

describe('replaceOptimisticTrackHome', () => {
  it('swaps the optimistic entry for the real track in place', () => {
    const data = replaceOptimisticTrackHome(
      listResponse([trackResponse('opt'), trackResponse('b')]),
      'opt',
      trackResponse('real'),
    );

    expect(data?.items.map((t) => t.id)).toEqual(['real', 'b']);
    expect(data?.total).toBe(2);
  });

  it('dedupes and decrements the total when the real track already exists', () => {
    const data = replaceOptimisticTrackHome(
      listResponse([trackResponse('opt'), trackResponse('real')]),
      'opt',
      trackResponse('real'),
    );

    expect(data?.items.map((t) => t.id)).toEqual(['real']);
    expect(data?.total).toBe(1);
  });

  it('leaves undefined data untouched', () => {
    expect(replaceOptimisticTrackHome(undefined, 'opt', trackResponse('real'))).toBeUndefined();
  });
});
