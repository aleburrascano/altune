import { trackToDiscoveryResult } from '../track-to-discovery';
import type { TrackResponse } from '@shared/api-client/types';

function makeTrack(overrides: Partial<TrackResponse> = {}): TrackResponse {
  return {
    id: 'track-1',
    title: 'Song',
    artist: 'Artist',
    album: null,
    duration_seconds: null,
    added_at: '2026-06-24T00:00:00Z',
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

describe('trackToDiscoveryResult', () => {
  it('maps core fields with high confidence and no sources', () => {
    const result = trackToDiscoveryResult(makeTrack({ artwork_url: 'http://art' }));
    expect(result.kind).toBe('track');
    expect(result.title).toBe('Song');
    expect(result.subtitle).toBe('Artist');
    expect(result.image_url).toBe('http://art');
    expect(result.confidence).toBe('high');
    expect(result.sources).toEqual([]);
    expect(result.extras.track_id).toBe('track-1');
    expect(result.extras.acquisition_status).toBe('ready');
  });

  it('omits optional extras when the track lacks them', () => {
    const { extras } = trackToDiscoveryResult(makeTrack());
    expect(extras).not.toHaveProperty('album');
    expect(extras).not.toHaveProperty('duration_seconds');
    expect(extras).not.toHaveProperty('track_position');
  });

  it('includes track_position when the track carries a number', () => {
    const { extras } = trackToDiscoveryResult(
      makeTrack({ album: 'LP', duration_seconds: 200, track_number: 3 }),
    );
    expect(extras.album).toBe('LP');
    expect(extras.duration_seconds).toBe(200);
    expect(extras.track_position).toBe(3);
  });
});
