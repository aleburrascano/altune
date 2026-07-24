import type {
  CreateTrackRequest,
  ListTracksResponse,
  TrackResponse,
} from '@shared/api-client/types';
import type { DiscoveryResult } from '@shared/api-client/discovery';

import { trackExtras } from './extras-accessors';

export function toCreateTrackRequest(result: DiscoveryResult): CreateTrackRequest {
  const te = trackExtras(result.extras);
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
    track_number: te.trackPosition,
    ...(te.featuredArtists.length > 0 ? { featured_artists: te.featuredArtists } : {}),
    source_url: soundcloudUrl,
  };
}

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

export function insertOptimisticTrackHome(
  data: ListTracksResponse | undefined,
  track: TrackResponse,
): ListTracksResponse | undefined {
  if (data === undefined) return data;
  if (data.items.some((t) => t.id === track.id)) return data;
  return { ...data, items: [track, ...data.items], total: data.total + 1 };
}

export function replaceOptimisticTrackHome(
  data: ListTracksResponse | undefined,
  optimisticId: string,
  real: TrackResponse,
): ListTracksResponse | undefined {
  if (data === undefined) return data;
  const replaced = data.items.map((t) => (t.id === optimisticId ? real : t));
  const items = dedupById(replaced);
  return { ...data, items, total: Math.max(0, data.total - (replaced.length - items.length)) };
}

function dedupById<T extends { id: string }>(items: T[]): T[] {
  const seen = new Set<string>();
  return items.filter((t) => (seen.has(t.id) ? false : (seen.add(t.id), true)));
}
