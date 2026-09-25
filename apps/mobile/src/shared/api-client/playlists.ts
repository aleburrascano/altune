import { apiFetch, apiSend, signalInit } from './index';
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
} from './types';
import {
  asCount,
  asNumber,
  asRecord,
  asString,
  parseArray,
  parseListEnvelope,
} from './wireDecoders';

function buildPlaylistResponse(r: Record<string, unknown>, at: string): PlaylistResponse {
  return {
    id: asPlaylistId(asString(r.id, `${at}.id`)),
    name: asString(r.name, `${at}.name`),
    track_count: asNumber(r.track_count, `${at}.track_count`),
    preview_artwork_urls: parseArray(
      r.preview_artwork_urls,
      `${at}.preview_artwork_urls`,
      asString,
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
  return parseListEnvelope(r, at, parsePlaylistResponse);
}

function parsePlaylistDetailResponse(
  value: unknown,
  at = 'PlaylistDetailResponse',
): PlaylistDetailResponse {
  const r = asRecord(value, at);
  return {
    ...buildPlaylistResponse(r, at),
    total_duration_seconds: asNumber(r.total_duration_seconds, `${at}.total_duration_seconds`),
    tracks: parseArray(r.tracks, `${at}.tracks`, parseTrackResponse),
  };
}

function parseAddTracksToPlaylistResponse(
  value: unknown,
  at = 'AddTracksToPlaylistResponse',
): AddTracksToPlaylistResponse {
  const r = asRecord(value, at);
  return {
    added: asCount(r.added, `${at}.added`),
    skipped: asCount(r.skipped, `${at}.skipped`),
  };
}

function parseRemoveTracksFromPlaylistResponse(
  value: unknown,
  at = 'RemoveTracksFromPlaylistResponse',
): RemoveTracksFromPlaylistResponse {
  const r = asRecord(value, at);
  return { removed: asCount(r.removed, `${at}.removed`) };
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
      signalInit(signal),
    ),
  );
}

export async function getPlaylist(id: PlaylistId): Promise<PlaylistDetailResponse> {
  return parsePlaylistDetailResponse(await apiFetch<unknown>(`/v1/playlists/${idPathSegment(id)}`));
}

export async function createPlaylist(body: CreatePlaylistRequest): Promise<PlaylistResponse> {
  return parsePlaylistResponse(await apiSend<unknown>('/v1/playlists', 'POST', body));
}

export async function renamePlaylist(id: PlaylistId, name: string): Promise<PlaylistResponse> {
  return parsePlaylistResponse(
    await apiSend<unknown>(`/v1/playlists/${idPathSegment(id)}`, 'PATCH', { name }),
  );
}

export async function deletePlaylist(id: PlaylistId): Promise<void> {
  await apiFetch<void>(`/v1/playlists/${idPathSegment(id)}`, { method: 'DELETE' });
}

export async function addTracksToPlaylist(
  playlistId: PlaylistId,
  body: AddTracksToPlaylistRequest,
): Promise<AddTracksToPlaylistResponse> {
  return parseAddTracksToPlaylistResponse(
    await apiSend<unknown>(`/v1/playlists/${idPathSegment(playlistId)}/tracks/batch`, 'POST', body),
  );
}

export async function removeTracksFromPlaylist(
  playlistId: PlaylistId,
  body: RemoveTracksFromPlaylistRequest,
): Promise<RemoveTracksFromPlaylistResponse> {
  return parseRemoveTracksFromPlaylistResponse(
    await apiSend<unknown>(`/v1/playlists/${idPathSegment(playlistId)}/tracks`, 'DELETE', body),
  );
}
