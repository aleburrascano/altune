import type { TrackResponse } from '@shared/api-client/types';

import { buildPlayableQueue } from '../playFromList';

function trackResponse(overrides: Partial<TrackResponse> = {}): TrackResponse {
  return {
    id: 'track-1',
    title: 'Title',
    artist: 'Artist',
    album: null,
    duration_seconds: 200,
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

describe('buildPlayableQueue', () => {
  it('filters out unplayable Tracks and keeps the rest in order', () => {
    const tracks = [
      trackResponse({ id: 'a', acquisition_status: 'ready' }),
      trackResponse({ id: 'b', acquisition_status: 'pending' }),
      trackResponse({ id: 'c', acquisition_status: 'failed' }),
      trackResponse({ id: 'd', acquisition_status: 'ready' }),
    ];

    const { playable } = buildPlayableQueue(tracks, 'a');

    expect(playable.map((t) => t.source)).toEqual([
      { kind: 'library', trackId: 'a' },
      { kind: 'library', trackId: 'd' },
    ]);
  });

  it('finds the start index of the tapped Track within the filtered, playable list', () => {
    const tracks = [
      trackResponse({ id: 'a', acquisition_status: 'pending' }),
      trackResponse({ id: 'b', acquisition_status: 'ready' }),
      trackResponse({ id: 'c', acquisition_status: 'ready' }),
    ];

    expect(buildPlayableQueue(tracks, 'c').startIndex).toBe(1);
  });

  it('falls back to index 0 when the tapped Track became unacquirable between render and tap', () => {
    const tracks = [
      trackResponse({ id: 'a', acquisition_status: 'ready' }),
      trackResponse({ id: 'b', acquisition_status: 'ready' }),
    ];

    const { startIndex } = buildPlayableQueue(tracks, 'no-longer-ready');

    expect(startIndex).toBe(0);
  });

  it('falls back to index 0 for a deliberately empty target id, as used for a shuffle-all action', () => {
    const tracks = [
      trackResponse({ id: 'a', acquisition_status: 'ready' }),
      trackResponse({ id: 'b', acquisition_status: 'ready' }),
    ];

    const { startIndex } = buildPlayableQueue(tracks, '');

    expect(startIndex).toBe(0);
  });

  it('returns an empty playable list when every Track is unacquirable', () => {
    const tracks = [
      trackResponse({ id: 'a', acquisition_status: 'pending' }),
      trackResponse({ id: 'b', acquisition_status: 'failed' }),
    ];

    const { playable, startIndex } = buildPlayableQueue(tracks, 'a');

    expect(playable).toEqual([]);
    expect(startIndex).toBe(0);
  });

  it('returns an empty playable list for an empty Track list', () => {
    const { playable, startIndex } = buildPlayableQueue([], 'anything');

    expect(playable).toEqual([]);
    expect(startIndex).toBe(0);
  });
});
