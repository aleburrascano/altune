import type { QueryClient } from '@tanstack/react-query';

import { playlistKeys } from '@shared/lib/query-keys';

import {
  asPlaylistIdOrNull,
  asString,
  asTrackIdOrNull,
  stringArray,
  type ServerEventHandler,
  type ServerEventHandlers,
} from './eventPayload';
import {
  patchPlaylistName,
  removeTrackFromPlaylistCache,
  removeTracksFromPlaylistCache,
  reorderPlaylistCache,
} from './playlistCachePatch';
import type { ServerEvent } from './sse-client';

type PlaylistEventType =
  | 'track_added_to_playlist'
  | 'tracks_added_to_playlist'
  | 'track_removed_from_playlist'
  | 'tracks_removed_from_playlist'
  | 'playlist_created'
  | 'playlist_deleted'
  | 'playlist_renamed'
  | 'playlist_reordered';

function invalidateKeys(keys: readonly (readonly string[])[]): ServerEventHandler {
  return (queryClient) => {
    for (const queryKey of keys) {
      void queryClient.invalidateQueries({ queryKey });
    }
  };
}

function handlePlaylistRenamed(queryClient: QueryClient, event: ServerEvent): void {
  const playlistId = asPlaylistIdOrNull(event.data.playlist_id);
  const name = asString(event.data.name);
  if (playlistId && name != null) patchPlaylistName(queryClient, playlistId, name);
}

function handleTrackRemovedFromPlaylist(queryClient: QueryClient, event: ServerEvent): void {
  const playlistId = asPlaylistIdOrNull(event.data.playlist_id);
  const trackId = asTrackIdOrNull(event.data.track_id);
  if (playlistId && trackId) removeTrackFromPlaylistCache(queryClient, playlistId, trackId);
}

function handleTracksRemovedFromPlaylist(queryClient: QueryClient, event: ServerEvent): void {
  const playlistId = asPlaylistIdOrNull(event.data.playlist_id);
  const trackIds = stringArray(event.data.track_ids);
  if (!playlistId || !trackIds) return;
  removeTracksFromPlaylistCache(queryClient, playlistId, trackIds);
}

function handlePlaylistReordered(queryClient: QueryClient, event: ServerEvent): void {
  const playlistId = asPlaylistIdOrNull(event.data.playlist_id);
  const trackIds = stringArray(event.data.track_ids);
  if (playlistId && trackIds) reorderPlaylistCache(queryClient, playlistId, trackIds);
}

export const PLAYLIST_HANDLERS: ServerEventHandlers<PlaylistEventType> = {
  track_added_to_playlist: invalidateKeys([playlistKeys.details, playlistKeys.list]),
  tracks_added_to_playlist: invalidateKeys([playlistKeys.details, playlistKeys.list]),
  track_removed_from_playlist: handleTrackRemovedFromPlaylist,
  tracks_removed_from_playlist: handleTracksRemovedFromPlaylist,
  playlist_created: invalidateKeys([playlistKeys.list]),
  playlist_deleted: invalidateKeys([playlistKeys.list, playlistKeys.details]),
  playlist_renamed: handlePlaylistRenamed,
  playlist_reordered: handlePlaylistReordered,
};
