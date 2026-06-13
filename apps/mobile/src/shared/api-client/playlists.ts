import { apiFetch } from './index';
import type {
  AddTrackToPlaylistRequest,
  CreatePlaylistRequest,
  ListPlaylistsResponse,
  PlaylistDetailResponse,
  PlaylistResponse,
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

export async function addTrackToPlaylist(
  playlistId: string,
  body: AddTrackToPlaylistRequest,
): Promise<void> {
  await apiFetch<void>(`/v1/playlists/${playlistId}/tracks`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

export async function removeTrackFromPlaylist(
  playlistId: string,
  trackId: string,
): Promise<void> {
  await apiFetch<void>(`/v1/playlists/${playlistId}/tracks/${trackId}`, {
    method: 'DELETE',
  });
}

// fallow-ignore-next-line unused-export
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
