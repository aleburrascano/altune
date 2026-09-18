import { apiFetch } from './index';
import { asPlaylistId, idPathSegment, type PlaylistId } from './ids';
import { withQuery } from './queryString';
import { parseTrackResponse } from './tracks';
import type {
  AddTracksToPlaylistRequest,
  AddTracksToPlaylistResponse,
  CreatePlaylistRequest,
  ListPlaylistsResponse,
  PlaylistDetailResponse,
  PlaylistResponse,
  RemoveTracksFromPlaylistRequest,
  RemoveTracksFromPlaylistResponse,
  ReorderTracksRequest,
} from './types';
import { asArray, asNumber, asRecord, asString } from './wireDecoders';

function buildPlaylistResponse(r: Record<string, unknown>, at: string): PlaylistResponse {
  return {
    id: asPlaylistId(asString(r.id, `${at}.id`)),
    name: asString(r.name, `${at}.name`),
    track_count: asNumber(r.track_count, `${at}.track_count`),
    preview_artwork_urls: asArray(r.preview_artwork_urls, `${at}.preview_artwork_urls`).map(
      (item, i) => asString(item, `${at}.preview_artwork_urls[${i}]`),
    ),
    created_at: asString(r.created_at, `${at}.created_at`),
    updated_at: asString(r.updated_at, `${at}.updated_at`),
  };
}

function parsePlaylistResponse(value: unknown, at = 'PlaylistResponse'): PlaylistResponse {
  return buildPlaylistResponse(asRecord(value, at), at);
}

function parseListPlaylistsResponse(
  value: unknown,
  at = 'ListPlaylistsResponse',
): ListPlaylistsResponse {
  const r = asRecord(value, at);
  return {
    items: asArray(r.items, `${at}.items`).map((item, i) =>
      parsePlaylistResponse(item, `${at}.items[${i}]`),
    ),
    total: asNumber(r.total, `${at}.total`),
  };
}

function parsePlaylistDetailResponse(
  value: unknown,
  at = 'PlaylistDetailResponse',
): PlaylistDetailResponse {
  const r = asRecord(value, at);
  return {
    ...buildPlaylistResponse(r, at),
    total_duration_seconds: asNumber(r.total_duration_seconds, `${at}.total_duration_seconds`),
    tracks: asArray(r.tracks, `${at}.tracks`).map((item, i) =>
      parseTrackResponse(item, `${at}.tracks[${i}]`),
    ),
  };
}

export type PlaylistPage = {
  /** Playlists to ask for. Omitted, the server picks its own page size. */
  limit?: number;
  /** Playlists to skip before the page starts. Omitted, the server starts at the first. */
  offset?: number;
};

function playlistPageParams(page: PlaylistPage): URLSearchParams {
  const params = new URLSearchParams();
  if (page.limit !== undefined) params.set('limit', String(page.limit));
  if (page.offset !== undefined) params.set('offset', String(page.offset));
  return params;
}

export async function getPlaylists(
  page: PlaylistPage = {},
  signal?: AbortSignal,
): Promise<ListPlaylistsResponse> {
  return parseListPlaylistsResponse(
    await apiFetch<unknown>(
      withQuery('/v1/playlists', playlistPageParams(page)),
      signal ? { signal } : undefined,
    ),
  );
}

export async function getPlaylist(id: PlaylistId): Promise<PlaylistDetailResponse> {
  return parsePlaylistDetailResponse(await apiFetch<unknown>(`/v1/playlists/${idPathSegment(id)}`));
}

export async function createPlaylist(body: CreatePlaylistRequest): Promise<PlaylistResponse> {
  return parsePlaylistResponse(
    await apiFetch<unknown>('/v1/playlists', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
  );
}

export async function renamePlaylist(id: PlaylistId, name: string): Promise<PlaylistResponse> {
  return parsePlaylistResponse(
    await apiFetch<unknown>(`/v1/playlists/${idPathSegment(id)}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    }),
  );
}

export async function deletePlaylist(id: PlaylistId): Promise<void> {
  await apiFetch<void>(`/v1/playlists/${idPathSegment(id)}`, { method: 'DELETE' });
}

export async function addTracksToPlaylist(
  playlistId: PlaylistId,
  body: AddTracksToPlaylistRequest,
): Promise<AddTracksToPlaylistResponse> {
  return apiFetch<AddTracksToPlaylistResponse>(
    `/v1/playlists/${idPathSegment(playlistId)}/tracks/batch`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    },
  );
}

export async function removeTracksFromPlaylist(
  playlistId: PlaylistId,
  body: RemoveTracksFromPlaylistRequest,
): Promise<RemoveTracksFromPlaylistResponse> {
  return apiFetch<RemoveTracksFromPlaylistResponse>(
    `/v1/playlists/${idPathSegment(playlistId)}/tracks`,
    {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    },
  );
}

export async function reorderPlaylistTracks(
  playlistId: PlaylistId,
  body: ReorderTracksRequest,
): Promise<void> {
  await apiFetch<void>(`/v1/playlists/${idPathSegment(playlistId)}/tracks/reorder`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}
