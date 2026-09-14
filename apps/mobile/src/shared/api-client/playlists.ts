import { ContractError } from './errors';
import { apiFetch } from './index';
import { parsePlaylistId, type PlaylistId } from './ids';
import {
  parseListPlaylistsResponse,
  parsePlaylistDetailResponse,
  parsePlaylistResponse,
} from './parse';
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

// Every playlist id reaches a request path through this one function, so no call site can forget
// to escape it: a `/`, `?` or `#` stays inside its one path segment instead of changing the route.
// Escaping alone cannot neutralise a `..` id (URL resolution collapses it, even as `%2e%2e`), so an
// id that isn't a safe shape is refused before any request is sent. Call sites keep the literal
// `/v1/playlists/${...}` template so the routes contract test can still read each path.
function playlistSegment(id: PlaylistId): string {
  const parsed = parsePlaylistId(id);
  if (!parsed.ok) throw new ContractError('PlaylistId', 'not a safe URL path segment');
  return encodeURIComponent(parsed.id);
}

export async function getPlaylists(): Promise<ListPlaylistsResponse> {
  return parseListPlaylistsResponse(await apiFetch<unknown>('/v1/playlists'));
}

export async function getPlaylist(id: PlaylistId): Promise<PlaylistDetailResponse> {
  return parsePlaylistDetailResponse(
    await apiFetch<unknown>(`/v1/playlists/${playlistSegment(id)}`),
  );
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
    await apiFetch<unknown>(`/v1/playlists/${playlistSegment(id)}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    }),
  );
}

export async function deletePlaylist(id: PlaylistId): Promise<void> {
  await apiFetch<void>(`/v1/playlists/${playlistSegment(id)}`, { method: 'DELETE' });
}

export async function addTracksToPlaylist(
  playlistId: PlaylistId,
  body: AddTracksToPlaylistRequest,
): Promise<AddTracksToPlaylistResponse> {
  return apiFetch<AddTracksToPlaylistResponse>(
    `/v1/playlists/${playlistSegment(playlistId)}/tracks/batch`,
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
    `/v1/playlists/${playlistSegment(playlistId)}/tracks`,
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
  await apiFetch<void>(`/v1/playlists/${playlistSegment(playlistId)}/tracks/reorder`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}
