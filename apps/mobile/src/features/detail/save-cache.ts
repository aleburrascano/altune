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

import { trackExtras } from './extras-accessors';

const PAGE_SIZE = 50;

/** Map a tapped track result into the POST body. Artist comes from subtitle;
 * album + duration are narrowed out of the untyped extras; artwork from the
 * result image. Title/artist invariants are enforced server-side (and the Save
 * button is disabled when subtitle is null — slice 16). */
export function toCreateTrackRequest(result: DiscoveryResult): CreateTrackRequest {
  const te = trackExtras(result.extras);
  // The SoundCloud permalink is a directly-downloadable source: when present, the
  // backend acquires that exact track instead of re-searching by title/artist.
  const soundcloudUrl = result.sources.find((s) => s.provider === 'soundcloud')?.url ?? null;
  return {
    title: result.title,
    artist: result.subtitle ?? '',
    album: te.album,
    duration_seconds: te.durationSeconds != null ? Math.floor(te.durationSeconds) : null,
    artwork_url: result.image_url,
    isrc: te.isrc,
    year: te.year,
    genre: te.genre,
    album_artist: te.albumArtist,
    ...(te.featuredArtists.length > 0 ? { featured_artists: te.featuredArtists } : {}),
    source_url: soundcloudUrl,
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
    failure_reason: null,
    year: body.year ?? null,
    genre: body.genre ?? null,
    track_number: null,
    album_artist: body.album_artist ?? null,
    isrc: body.isrc ?? null,
    audio_ref: null,
    ...(body.featured_artists ? { featured_artists: body.featured_artists } : {}),
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

/**
 * Upsert the optimistic placeholder into the `['library-home']` snapshot too,
 * creating the snapshot if it doesn't exist yet. Readers (the detail
 * save-control via findTrackInLibraryCache, and the Activity Dock) consult
 * library-home first, so without this a save from Detail shows no feedback once
 * Library has been opened. Idempotent on the placeholder id.
 */
export function insertOptimisticTrackHome(
  data: ListTracksResponse | undefined,
  track: TrackResponse,
): ListTracksResponse {
  if (data === undefined) {
    return { items: [track], total: 1, limit: PAGE_SIZE, offset: 0, has_more: false };
  }
  if (data.items.some((t) => t.id === track.id)) return data;
  return { ...data, items: [track, ...data.items], total: data.total + 1 };
}

/**
 * Swap the optimistic placeholder for the real server row once the POST returns,
 * so subsequent acquisition SSE events (which carry the real track id) match it.
 */
export function replaceOptimisticTrackInfinite(
  data: InfiniteData<ListTracksResponse> | undefined,
  optimisticId: string,
  real: TrackResponse,
): InfiniteData<ListTracksResponse> | undefined {
  if (data === undefined) return data;
  return {
    ...data,
    pages: data.pages.map((page) => ({
      ...page,
      items: page.items.map((t) => (t.id === optimisticId ? real : t)),
    })),
  };
}

export function replaceOptimisticTrackHome(
  data: ListTracksResponse | undefined,
  optimisticId: string,
  real: TrackResponse,
): ListTracksResponse | undefined {
  if (data === undefined) return data;
  return { ...data, items: data.items.map((t) => (t.id === optimisticId ? real : t)) };
}
