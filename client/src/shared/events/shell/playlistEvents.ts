import type { EventCache } from '@shared/events/core/eventCache';
import {
  asString,
  asStringArray,
  type ServerEventHandlers,
} from '@shared/events/core/serverEventTypes';
import type { ServerEvent } from '@shared/events/core/sseFrames';
import { invalidateForEvent, invalidatesOn } from '@shared/events/shell/invalidateOnEvent';
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

function handlePlaylistRenamed(cache: EventCache, event: ServerEvent): void {
  const playlistId = asPlaylistIdOrNull(event.data.playlist_id);
  const name = asString(event.data.name);
  if (playlistId && name != null) patchPlaylistName(cache, playlistId, name);
}

function handleTrackRemovedFromPlaylist(cache: EventCache, event: ServerEvent): void {
  const playlistId = asPlaylistIdOrNull(event.data.playlist_id);
  const trackId = asTrackIdOrNull(event.data.track_id);
  if (playlistId && trackId) removeTrackFromPlaylistCache(cache, playlistId, trackId);
  invalidateForEvent(cache, 'track_removed_from_playlist');
}

function handleTracksRemovedFromPlaylist(cache: EventCache, event: ServerEvent): void {
  const playlistId = asPlaylistIdOrNull(event.data.playlist_id);
  const trackIds = asStringArray(event.data.track_ids);
  if (playlistId && trackIds) removeTracksFromPlaylistCache(cache, playlistId, trackIds);
  invalidateForEvent(cache, 'tracks_removed_from_playlist');
}

function handlePlaylistReordered(cache: EventCache, event: ServerEvent): void {
  const playlistId = asPlaylistIdOrNull(event.data.playlist_id);
  const trackIds = asStringArray(event.data.track_ids);
  if (playlistId && trackIds) reorderPlaylistCache(cache, playlistId, trackIds);
}

export const PLAYLIST_HANDLERS: ServerEventHandlers<PlaylistEventType> = {
  track_added_to_playlist: invalidatesOn('track_added_to_playlist'),
  tracks_added_to_playlist: invalidatesOn('tracks_added_to_playlist'),
  track_removed_from_playlist: handleTrackRemovedFromPlaylist,
  tracks_removed_from_playlist: handleTracksRemovedFromPlaylist,
  playlist_created: invalidatesOn('playlist_created'),
  playlist_deleted: invalidatesOn('playlist_deleted'),
  playlist_renamed: handlePlaylistRenamed,
  playlist_reordered: handlePlaylistReordered,
};
