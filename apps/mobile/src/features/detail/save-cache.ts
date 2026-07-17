/**
 * Pure cache transforms for the optimistic save flow.
 *
 * The library is the ['library-home'] snapshot (a ListTracksResponse).
 * Optimistically inserting a saved track = prepend it and bump the total. Kept
 * pure + RN-free so it unit-tests without a QueryClient (matches the repo's
 * helper pattern); the hook in hooks/useSaveTrack.ts wires these into
 * onMutate/onError.
 */

import type { CreateTrackRequest, ListTracksResponse, TrackResponse } from '@shared/api-client/types';
import type { DiscoveryResult } from '@shared/api-client/discovery';

import { trackExtras } from './extras-accessors';

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
    // Present when saving from an album context (the provider tracklist carries
    // the position); null for a bare search-result save. Populating it fixes the
    // library falling back to 1..N ordering.
    track_number: te.trackPosition,
    ...(te.featuredArtists.length > 0 ? { featured_artists: te.featuredArtists } : {}),
    source_url: soundcloudUrl,
  };
}

/** Build the placeholder row shown immediately while the POST is in flight. */
export function optimisticTrack(body: CreateTrackRequest, addedAt: string): TrackResponse {
  return {
    id: `optimistic:${body.title}${body.artist}`,
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
    track_number: body.track_number ?? null,
    album_artist: body.album_artist ?? null,
    isrc: body.isrc ?? null,
    audio_ref: null,
    ...(body.featured_artists ? { featured_artists: body.featured_artists } : {}),
  };
}

/**
 * Upsert the optimistic placeholder into the `['library-home']` snapshot.
 * Readers (the detail save-control via findTrackInLibraryCache, and the Activity
 * Dock) consult library-home, so without this a save from Detail shows no
 * feedback once Library has been opened. Idempotent on the placeholder id.
 *
 * AIDEV-WARNING: never seed a snapshot from `undefined` — an absent cache means
 * the library failed to load (401 / network error) or hasn't fetched yet, NOT
 * that it's empty (an empty library is `{items: []}`). Seeding here fabricated a
 * `total: 1` library out of an error state and, because setQueryData also flips
 * the query to `success`, replaced the error screen with a confident lie that
 * `staleTime: Infinity` then pinned for the whole session — a user with 273
 * saved tracks saw exactly one track, one album, one artist. Returning undefined
 * leaves the cache untouched (setQueryData no-ops on it) so the real fetch wins.
 */
export function insertOptimisticTrackHome(
  data: ListTracksResponse | undefined,
  track: TrackResponse,
): ListTracksResponse | undefined {
  if (data === undefined) return data;
  if (data.items.some((t) => t.id === track.id)) return data;
  return { ...data, items: [track, ...data.items], total: data.total + 1 };
}

/**
 * Swap the optimistic placeholder for the real server row once the POST returns,
 * so subsequent acquisition SSE events (which carry the real track id) match it.
 */
export function replaceOptimisticTrackHome(
  data: ListTracksResponse | undefined,
  optimisticId: string,
  real: TrackResponse,
): ListTracksResponse | undefined {
  if (data === undefined) return data;
  // The acquisition SSE (track_added_to_library) may have already upserted the
  // real row by the time onSuccess runs; a bare replace would then leave two
  // entries with real.id. Replace the placeholder, then dedup by id (keep first)
  // and correct total by however many duplicates were removed.
  const replaced = data.items.map((t) => (t.id === optimisticId ? real : t));
  const items = dedupById(replaced);
  return { ...data, items, total: Math.max(0, data.total - (replaced.length - items.length)) };
}

function dedupById<T extends { id: string }>(items: T[]): T[] {
  const seen = new Set<string>();
  return items.filter((t) => (seen.has(t.id) ? false : (seen.add(t.id), true)));
}
