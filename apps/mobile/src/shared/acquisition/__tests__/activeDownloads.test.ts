import type { InfiniteData } from '@tanstack/react-query';

import type {
  AcquisitionStatus,
  ListTracksResponse,
  TrackResponse,
} from '@shared/api-client/types';

import { deriveActiveDownloads } from '../activeDownloads';

function track(id: string, status: AcquisitionStatus): TrackResponse {
  return {
    id,
    title: `Title ${id}`,
    artist: 'M83',
    album: null,
    duration_seconds: null,
    added_at: '2026-06-30T00:00:00Z',
    acquisition_status: status,
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

function home(items: TrackResponse[]): ListTracksResponse {
  return { items, total: items.length, limit: 50, offset: 0, has_more: false };
}

describe('deriveActiveDownloads', () => {
  it('returns only pending tracks from the home snapshot', () => {
    const result = deriveActiveDownloads(
      home([track('a', 'pending'), track('b', 'ready'), track('c', 'failed')]),
      undefined,
    );
    expect(result.map((d) => d.id)).toEqual(['a']);
    expect(result[0]).toMatchObject({ title: 'Title a', artist: 'M83', artworkUrl: null });
  });

  it('merges the infinite-query pages and dedupes by id', () => {
    const infinite: InfiniteData<ListTracksResponse> = {
      pageParams: [0],
      pages: [home([track('a', 'pending'), track('d', 'pending')])],
    };
    const result = deriveActiveDownloads(home([track('a', 'pending')]), infinite);
    expect(result.map((d) => d.id).sort()).toEqual(['a', 'd']);
  });

  it('is empty when both caches are absent', () => {
    expect(deriveActiveDownloads(undefined, undefined)).toEqual([]);
  });
});
