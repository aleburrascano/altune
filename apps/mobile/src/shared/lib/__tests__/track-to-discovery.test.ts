import { trackToDiscoveryResult } from '../track-to-discovery';

import type { FeaturedArtist, TrackResponse } from '@shared/api-client/types';

function makeTrack(overrides: Partial<TrackResponse> = {}): TrackResponse {
  return {
    id: 'track-1',
    title: 'Midnight City',
    artist: 'M83',
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
    ...overrides,
  };
}

describe('trackToDiscoveryResult — fixed shape', () => {
  it('maps kind, title, subtitle, image_url, confidence and sources for a SAVED library track', () => {
    const track = makeTrack({ title: 'Midnight City', artist: 'M83', artwork_url: 'https://a.example/art.jpg' });
    const result = trackToDiscoveryResult(track);
    expect(result.kind).toBe('track');
    expect(result.title).toBe('Midnight City');
    expect(result.subtitle).toBe('M83');
    expect(result.image_url).toBe('https://a.example/art.jpg');
    expect(result.confidence).toBe('high');
    expect(result.sources).toEqual([]);
  });

  it('image_url falls back to null when artwork_url is null', () => {
    expect(trackToDiscoveryResult(makeTrack({ artwork_url: null })).image_url).toBeNull();
  });

  it('image_url falls back to null when artwork_url is an empty string, so Artwork renders its placeholder rather than requesting an empty uri', () => {
    expect(trackToDiscoveryResult(makeTrack({ artwork_url: '' })).image_url).toBeNull();
  });

  it('extras always carries acquisition_status and track_id', () => {
    const track = makeTrack({ acquisition_status: 'failed', id: 'track-42' });
    const result = trackToDiscoveryResult(track);
    expect(result.extras['acquisition_status']).toBe('failed');
    expect(result.extras['track_id']).toBe('track-42');
  });
});

describe('trackToDiscoveryResult — album spread arm: != null, so "" survives', () => {
  it('omits the album key when album is null', () => {
    expect(trackToDiscoveryResult(makeTrack({ album: null })).extras).not.toHaveProperty('album');
  });

  it('includes album as the empty string, the falsy-but-valid boundary', () => {
    expect(trackToDiscoveryResult(makeTrack({ album: '' })).extras['album']).toBe('');
  });

  it('includes a non-empty album', () => {
    expect(trackToDiscoveryResult(makeTrack({ album: 'Hurry Up, We\'re Dreaming' })).extras['album']).toBe(
      "Hurry Up, We're Dreaming",
    );
  });
});

describe('trackToDiscoveryResult — duration_seconds spread arm: != null, so 0 survives', () => {
  it('omits duration_seconds when null', () => {
    expect(trackToDiscoveryResult(makeTrack({ duration_seconds: null })).extras).not.toHaveProperty(
      'duration_seconds',
    );
  });

  it('includes duration_seconds of exactly 0, the falsy-but-valid boundary', () => {
    expect(trackToDiscoveryResult(makeTrack({ duration_seconds: 0 })).extras['duration_seconds']).toBe(0);
  });

  it('includes a positive duration_seconds', () => {
    expect(trackToDiscoveryResult(makeTrack({ duration_seconds: 245 })).extras['duration_seconds']).toBe(
      245,
    );
  });
});

describe('trackToDiscoveryResult — track_number spread arm: != null, so 0 survives, renamed to track_position', () => {
  it('omits track_position when track_number is null', () => {
    expect(trackToDiscoveryResult(makeTrack({ track_number: null })).extras).not.toHaveProperty(
      'track_position',
    );
  });

  it('includes track_position of exactly 0, the falsy-but-valid boundary', () => {
    expect(trackToDiscoveryResult(makeTrack({ track_number: 0 })).extras['track_position']).toBe(0);
  });

  it('includes a positive track_number as track_position', () => {
    expect(trackToDiscoveryResult(makeTrack({ track_number: 5 })).extras['track_position']).toBe(5);
  });
});

describe('trackToDiscoveryResult — featured_artists spread arm: truthiness + length > 0, unlike the other three', () => {
  it('omits featured_artists when the field is absent (undefined), a real optional-field arrival shape', () => {
    const track = makeTrack();
    delete (track as { featured_artists?: FeaturedArtist[] }).featured_artists;
    expect(trackToDiscoveryResult(track).extras).not.toHaveProperty('featured_artists');
  });

  it('omits featured_artists when it is an empty array, unlike the 0/"" boundaries above', () => {
    expect(trackToDiscoveryResult(makeTrack({ featured_artists: [] })).extras).not.toHaveProperty(
      'featured_artists',
    );
  });

  it('includes featured_artists verbatim when at least one credit is present', () => {
    const featured: FeaturedArtist[] = [{ name: 'Rihanna', mbid: null, deezer_id: 564 }];
    expect(trackToDiscoveryResult(makeTrack({ featured_artists: featured })).extras['featured_artists']).toEqual(
      featured,
    );
  });
});

describe('trackToDiscoveryResult — idempotence / replay, no input mutation', () => {
  it('applying twice to the same frozen TrackResponse yields deep-equal results and never throws', () => {
    const track = Object.freeze(
      makeTrack({
        album: 'Hurry Up, We\'re Dreaming',
        duration_seconds: 0,
        track_number: 3,
        featured_artists: [{ name: 'Rihanna', mbid: null, deezer_id: 564 }],
      }),
    );
    expect(() => trackToDiscoveryResult(track)).not.toThrow();
    const first = trackToDiscoveryResult(track);
    const second = trackToDiscoveryResult(track);
    expect(second).toEqual(first);
  });

  it('does not mutate the input track object', () => {
    const track = makeTrack({ album: 'Rumours', featured_artists: [] });
    const before = JSON.parse(JSON.stringify(track));
    trackToDiscoveryResult(track);
    expect(track).toEqual(before);
  });
});

describe('trackToDiscoveryResult — FeaturingScreen path: every track has non-empty featured_artists by construction', () => {
  it('spreads the artist-filtered query\'s featured_artists into extras for every track', () => {
    const featured: FeaturedArtist[] = [
      { name: 'Drake', mbid: 'mb-drake', deezer_id: null },
      { name: 'SZA', mbid: null, deezer_id: 7 },
    ];
    const track = makeTrack({ artist: 'Rihanna', featured_artists: featured });
    const result = trackToDiscoveryResult(track);
    expect(result.extras['featured_artists']).toEqual(featured);
  });
});
