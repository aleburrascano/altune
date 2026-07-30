import type { QueueStateCurrentTrack } from '@shared/api-client/playback';
import type { TrackResponse } from '@shared/api-client/types';

import { currentTrackToPlaybackTrack, toPlaybackTrack } from '../toPlaybackTrack';

function trackResponse(overrides: Partial<TrackResponse> = {}): TrackResponse {
  return {
    id: 'track-1',
    title: 'Title',
    artist: 'Artist',
    album: 'Album',
    duration_seconds: 210,
    added_at: '2026-01-01T00:00:00Z',
    acquisition_status: 'ready',
    artwork_url: 'https://cdn.example.com/art.jpg',
    failure_reason: null,
    year: 2020,
    genre: 'Rock',
    track_number: 3,
    album_artist: 'Album Artist',
    isrc: 'US1234567890',
    audio_ref: 'ref-1',
    featured_artists: [{ name: 'Featured', mbid: null, deezer_id: null }],
    ...overrides,
  };
}

function currentTrack(overrides: Partial<QueueStateCurrentTrack> = {}): QueueStateCurrentTrack {
  return {
    id: 'track-1',
    title: 'Title',
    artist: 'Artist',
    artwork_url: 'https://cdn.example.com/art.jpg',
    duration_seconds: 210,
    acquisition_status: 'ready',
    ...overrides,
  };
}

describe('toPlaybackTrack', () => {
  it('maps a fully populated library Track into a playback Track', () => {
    expect(toPlaybackTrack(trackResponse())).toEqual({
      source: { kind: 'library', trackId: 'track-1' },
      title: 'Title',
      artist: 'Artist',
      artworkUrl: 'https://cdn.example.com/art.jpg',
      durationSeconds: 210,
      featuredArtists: [{ name: 'Featured', mbid: null, deezer_id: null }],
    });
  });

  it('falls back to null artwork when artwork_url is missing', () => {
    expect(toPlaybackTrack(trackResponse({ artwork_url: null })).artworkUrl).toBeNull();
  });

  it('falls back to undefined duration when duration_seconds is missing', () => {
    expect(
      toPlaybackTrack(trackResponse({ duration_seconds: null })).durationSeconds,
    ).toBeUndefined();
  });

  it('derives a library source keyed by the Track id', () => {
    expect(toPlaybackTrack(trackResponse({ id: 'track-42' })).source).toEqual({
      kind: 'library',
      trackId: 'track-42',
    });
  });
});

describe('currentTrackToPlaybackTrack', () => {
  it('maps a fully populated now-playing snapshot into a playback Track', () => {
    expect(currentTrackToPlaybackTrack(currentTrack())).toEqual({
      source: { kind: 'library', trackId: 'track-1' },
      title: 'Title',
      artist: 'Artist',
      artworkUrl: 'https://cdn.example.com/art.jpg',
      durationSeconds: 210,
    });
  });

  it('falls back to null artwork when artwork_url is missing', () => {
    expect(
      currentTrackToPlaybackTrack(currentTrack({ artwork_url: null })).artworkUrl,
    ).toBeNull();
  });

  it('falls back to undefined duration when duration_seconds is missing', () => {
    expect(
      currentTrackToPlaybackTrack(currentTrack({ duration_seconds: null })).durationSeconds,
    ).toBeUndefined();
  });

  it('agrees with toPlaybackTrack on identity for the same Track id', () => {
    const fromLibraryRebuild = toPlaybackTrack(trackResponse({ id: 'shared-id' }));
    const fromResumePlaceholder = currentTrackToPlaybackTrack(currentTrack({ id: 'shared-id' }));

    expect(fromResumePlaceholder.source).toEqual(fromLibraryRebuild.source);
  });
});
