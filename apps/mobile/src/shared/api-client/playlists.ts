import { apiFetch } from './index';
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

export async function getPlaylists(): Promise<ListPlaylistsResponse> {
  return apiFetch<ListPlaylistsResponse>('/v1/playlists');
}

export async function getPlaylist(id: string): Promise<PlaylistDetailResponse> {
  return apiFetch<PlaylistDetailResponse>(`/v1/playlists/${id}`);
}

export async function createPlaylist(body: CreatePlaylistRequest): Promise<PlaylistResponse> {
  return apiFetch<PlaylistResponse>('/v1/playlists', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

export async function renamePlaylist(id: string, name: string): Promise<PlaylistResponse> {
  return apiFetch<PlaylistResponse>(`/v1/playlists/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  });
}

export async function deletePlaylist(id: string): Promise<void> {
  await apiFetch<void>(`/v1/playlists/${id}`, { method: 'DELETE' });
}

export async function addTracksToPlaylist(
  playlistId: string,
  body: AddTracksToPlaylistRequest,
): Promise<AddTracksToPlaylistResponse> {
  return apiFetch<AddTracksToPlaylistResponse>(
    `/v1/playlists/${encodeURIComponent(playlistId)}/tracks/batch`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    },
  );
}

export async function removeTracksFromPlaylist(
  playlistId: string,
  body: RemoveTracksFromPlaylistRequest,
): Promise<RemoveTracksFromPlaylistResponse> {
  return apiFetch<RemoveTracksFromPlaylistResponse>(
    `/v1/playlists/${encodeURIComponent(playlistId)}/tracks`,
    {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    },
  );
}

export async function reorderPlaylistTracks(
  playlistId: string,
  body: ReorderTracksRequest,
): Promise<void> {
  await apiFetch<void>(`/v1/playlists/${playlistId}/tracks/reorder`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}
