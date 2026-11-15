import { playlistKeys } from '@shared/query';

import type { EventCache } from '@shared/events/core/eventCache';
import {
  asString,
  asStringArray,
  type ServerEventHandler,
  type ServerEventHandlers,
} from '@shared/events/core/serverEventTypes';
import type { ServerEvent } from '@shared/events/core/sseFrames';
import { asPlaylistIdOrNull, asTrackIdOrNull } from '@shared/events/shell/eventIds';
import {
  patchPlaylistName,
  removeTrackFromPlaylistCache,
  removeTracksFromPlaylistCache,
  reorderPlaylistCache,
} from '@shared/events/shell/playlistCachePatch';

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
  return (cache) => {
    for (const queryKey of keys) {
      void cache.invalidateQueries({ queryKey });
    }
  };
}

function handlePlaylistRenamed(cache: EventCache, event: ServerEvent): void {
  const playlistId = asPlaylistIdOrNull(event.data.playlist_id);
  const name = asString(event.data.name);
  if (playlistId && name != null) patchPlaylistName(cache, playlistId, name);
}

function handleTrackRemovedFromPlaylist(cache: EventCache, event: ServerEvent): void {
  const playlistId = asPlaylistIdOrNull(event.data.playlist_id);
  const trackId = asTrackIdOrNull(event.data.track_id);
  if (playlistId && trackId) removeTrackFromPlaylistCache(cache, playlistId, trackId);
}

function handleTracksRemovedFromPlaylist(cache: EventCache, event: ServerEvent): void {
  const playlistId = asPlaylistIdOrNull(event.data.playlist_id);
  const trackIds = asStringArray(event.data.track_ids);
  if (!playlistId || !trackIds) return;
  removeTracksFromPlaylistCache(cache, playlistId, trackIds);
}

function handlePlaylistReordered(cache: EventCache, event: ServerEvent): void {
  const playlistId = asPlaylistIdOrNull(event.data.playlist_id);
  const trackIds = asStringArray(event.data.track_ids);
  if (playlistId && trackIds) reorderPlaylistCache(cache, playlistId, trackIds);
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
