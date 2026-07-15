/**
 * getTracks + createTrack contract — types come back as declared, errors surface.
 *
 * Slice 8 of view-library (getTracks). createTrack added by view-result-detail
 * slice 10. RED: stub returns empty; GREEN wires real fetch.
 */

import { ApiError } from '../index';
import { createTrack, getTracks } from '../tracks';
import type { ListTracksResponse, TrackResponse } from '../types';

const _SAMPLE_TRACK: TrackResponse = {
  id: '11111111-1111-1111-1111-000000000001',
  title: 'Blinding Lights',
  artist: 'The Weeknd',
  album: 'After Hours',
  duration_seconds: 200,
  added_at: '2026-05-01T12:00:00Z',
  acquisition_status: 'pending',
  artwork_url: null,
  year: null,
  genre: null,
  track_number: null,
  album_artist: null,
  isrc: null,
  audio_ref: null,
  failure_reason: null,
};

const _SAMPLE_RESPONSE: ListTracksResponse = {
  items: [_SAMPLE_TRACK],
  total: 1,
  limit: 50,
  offset: 0,
  has_more: false,
};

function mockFetchOk(body: unknown): void {
  global.fetch = jest.fn().mockResolvedValue({
    ok: true,
    status: 200,
    json: async () => body,
  });
}

function mockFetchStatus(status: number): void {
  global.fetch = jest.fn().mockResolvedValue({
    ok: false,
    status,
    json: async () => ({}),
  });
}

afterEach(() => {
  jest.resetAllMocks();
});

describe('getTracks', () => {
  it('returns typed ListTracksResponse on 200', async () => {
    mockFetchOk(_SAMPLE_RESPONSE);
    const result = await getTracks({ limit: 50, offset: 0 });
    expect(result.total).toBe(1);
    expect(result.items).toHaveLength(1);
    expect(result.items[0]?.title).toBe('Blinding Lights');
    expect(result.items[0]?.album).toBe('After Hours');
  });

  it('passes limit and offset as query params', async () => {
    mockFetchOk(_SAMPLE_RESPONSE);
    await getTracks({ limit: 25, offset: 50 });
    expect(fetch).toHaveBeenCalledTimes(1);
    const url = (fetch as jest.Mock).mock.calls[0]?.[0] as string;
    expect(url).toContain('limit=25');
    expect(url).toContain('offset=50');
    expect(url).toContain('/v1/tracks');
  });

  it('throws ApiError on non-2xx response', async () => {
    mockFetchStatus(500);
    await expect(getTracks({ limit: 50, offset: 0 })).rejects.toBeInstanceOf(ApiError);
  });
});

describe('createTrack', () => {
  it('posts mapped body to /v1/tracks', async () => {
    mockFetchOk(_SAMPLE_TRACK);
    const result = await createTrack({
      title: 'Blinding Lights',
      artist: 'The Weeknd',
      album: 'After Hours',
      duration_seconds: 200,
      artwork_url: null,
      isrc: null,
      year: null,
      genre: null,
      album_artist: null,
      track_number: null,
    });
    expect(result.id).toBe(_SAMPLE_TRACK.id);
    expect(result.acquisition_status).toBe('pending');
    expect(fetch).toHaveBeenCalledTimes(1);
    const [url, init] = (fetch as jest.Mock).mock.calls[0] as [string, RequestInit];
    expect(url).toContain('/v1/tracks');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toMatchObject({
      title: 'Blinding Lights',
      artist: 'The Weeknd',
      album: 'After Hours',
    });
  });

  it('throws ApiError on non-2xx response', async () => {
    mockFetchStatus(400);
    await expect(
      createTrack({
        title: 'x',
        artist: 'y',
        album: null,
        duration_seconds: null,
        artwork_url: null,
        isrc: null,
        year: null,
        genre: null,
        album_artist: null,
        track_number: null,
      }),
    ).rejects.toBeInstanceOf(ApiError);
  });
});
