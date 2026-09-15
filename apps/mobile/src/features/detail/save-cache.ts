import { asTrackId, type TrackId } from '@shared/api-client/ids';
import type { CreateTrackRequest, TrackResponse } from '@shared/api-client/types';
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

// A placeholder id for a save still in flight. It stays deterministic per title+artist (a repeat
// save lands on the same row) but is hashed into the safe id shape, since track ids become URL
// path segments and file names and asTrackId refuses anything else.
function optimisticTrackId(body: CreateTrackRequest): TrackId {
  let hash = 0x811c9dc5;
  for (const ch of `${body.title}\u0000${body.artist}`) {
    hash ^= ch.codePointAt(0) ?? 0;
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return asTrackId(`optimistic-${hash.toString(16).padStart(8, '0')}`);
}

export function optimisticTrack(body: CreateTrackRequest, addedAt: string): TrackResponse {
  return {
    id: optimisticTrackId(body),
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
