/**
 * Pure cache transforms for the optimistic save flow.
 *
 * The library list is a React Query useInfiniteQuery keyed ['library'] holding
 * InfiniteData<ListTracksResponse>. Optimistically inserting a saved track =
 * prepend it to the first page and bump that page's total. Kept pure + RN-free
 * so it unit-tests without a QueryClient (matches the repo's helper pattern);
 * the hook in hooks/useSaveTrack.ts wires these into onMutate/onError.
 */

import type { InfiniteData } from '@tanstack/react-query';

import type { CreateTrackRequest, ListTracksResponse, TrackResponse } from '@shared/api-client/types';
import type { DiscoveryResult } from '@shared/api-client/discovery';

const PAGE_SIZE = 50;

/** Map a tapped track result into the POST body. Artist comes from subtitle;
 * album + duration are narrowed out of the untyped extras; artwork from the
 * result image. Title/artist invariants are enforced server-side (and the Save
 * button is disabled when subtitle is null — slice 16). */
export function toCreateTrackRequest(result: DiscoveryResult): CreateTrackRequest {
  const album = result.extras['album'];
  const duration = result.extras['duration_seconds'];
  return {
    title: result.title,
    artist: result.subtitle ?? '',
    album: typeof album === 'string' && album.length > 0 ? album : null,
    duration_seconds:
      typeof duration === 'number' && Number.isFinite(duration) ? Math.floor(duration) : null,
    artwork_url: result.image_url,
  };
}

/** Build the placeholder row shown immediately while the POST is in flight. */
export function optimisticTrack(body: CreateTrackRequest, addedAt: string): TrackResponse {
  return {
    id: `optimistic:${body.title}${body.artist}`,
    title: body.title,
    artist: body.artist,
    album: body.album,
    duration_seconds: body.duration_seconds,
    added_at: addedAt,
    acquisition_status: 'pending',
    artwork_url: body.artwork_url,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
  };
}

export function insertOptimisticTrack(
  data: InfiniteData<ListTracksResponse> | undefined,
  track: TrackResponse,
): InfiniteData<ListTracksResponse> {
  const first = data?.pages[0];
  if (data === undefined || first === undefined) {
    return {
      pageParams: [0],
      pages: [{ items: [track], total: 1, limit: PAGE_SIZE, offset: 0, has_more: false }],
    };
  }
  const updatedFirst: ListTracksResponse = {
    ...first,
    items: [track, ...first.items],
    total: first.total + 1,
  };
  return { ...data, pages: [updatedFirst, ...data.pages.slice(1)] };
}
