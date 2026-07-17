import { QueryClient } from '@tanstack/react-query';

import type {
  ListTracksResponse,
  PlaylistDetailResponse,
  TrackResponse,
} from '@shared/api-client/types';

import { patchTrackInCaches } from '../trackCachePatch';

function makeTrack(overrides: Partial<TrackResponse>): TrackResponse {
  return {
    id: 'track-1',
    title: 'Midnight City',
    artist: 'M83',
    album: null,
    duration_seconds: 243,
    added_at: '2026-06-30T00:00:00Z',
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

describe('patchTrackInCaches', () => {
  it('patches the track in the library-home snapshot', () => {
    const qc = new QueryClient();
    qc.setQueryData<ListTracksResponse>(['library-home'], {
      items: [makeTrack({ id: 'track-1' }), makeTrack({ id: 'track-2' })],
      total: 2,
      limit: 50,
      offset: 0,
      has_more: false,
    });

    patchTrackInCaches(qc, 'track-1', { acquisition_status: 'ready', audio_ref: 'ref-1' });

    const data = qc.getQueryData<ListTracksResponse>(['library-home']);
    expect(data?.items[0]).toMatchObject({ acquisition_status: 'ready', audio_ref: 'ref-1' });
    expect(data?.items[1]?.acquisition_status).toBe('pending');
  });

  it('patches the track in every cached playlist detail', () => {
    const qc = new QueryClient();
    const base = {
      id: 'pl-1',
      name: 'Faves',
      track_count: 1,
      preview_artwork_urls: [],
      created_at: '',
      updated_at: '',
    };
    qc.setQueryData<PlaylistDetailResponse>(['playlist', 'pl-1'], {
      ...base,
      tracks: [makeTrack({ id: 'track-1' })],
    });

    patchTrackInCaches(qc, 'track-1', { acquisition_status: 'failed', failure_reason: 'no source' });

    const data = qc.getQueryData<PlaylistDetailResponse>(['playlist', 'pl-1']);
    expect(data?.tracks[0]).toMatchObject({
      acquisition_status: 'failed',
      failure_reason: 'no source',
    });
  });

  it('is a no-op when no cache holds the track', () => {
    const qc = new QueryClient();
    expect(() => patchTrackInCaches(qc, 'ghost', { acquisition_status: 'ready' })).not.toThrow();
    expect(qc.getQueryData(['library-home'])).toBeUndefined();
  });
});
