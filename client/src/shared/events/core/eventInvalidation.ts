import { libraryKeys, playlistKeys } from '@shared/query';

import { TRACK_CACHE_FAMILIES } from '@shared/events/core/trackCacheShapes';
import type { ServerEventType } from '@shared/events/core/serverEventTypes';

type QueryKeys = readonly (readonly string[])[];

const LIBRARY_DERIVED: QueryKeys = [
  libraryKeys.albumsPrefix,
  libraryKeys.artistsPrefix,
  libraryKeys.summary,
  libraryKeys.lookupPrefix,
];

const RESYNC_KEYS: QueryKeys = [
  TRACK_CACHE_FAMILIES.pagedLibrary.prefix,
  TRACK_CACHE_FAMILIES.lookup.prefix,
  libraryKeys.albumsPrefix,
  libraryKeys.artistsPrefix,
  libraryKeys.summary,
  TRACK_CACHE_FAMILIES.featuring.prefix,
  playlistKeys.list,
  TRACK_CACHE_FAMILIES.playlistDetails.prefix,
];

export const EVENT_INVALIDATES: Record<ServerEventType, QueryKeys> = {
  resync: RESYNC_KEYS,
  track_added_to_library: LIBRARY_DERIVED,
  track_deleted: [...LIBRARY_DERIVED, playlistKeys.list],
  track_acquisition_started: [],
  track_acquisition_progress: [],
  track_acquisition_completed: [],
  track_acquisition_failed: [],
  track_replace_failed: [],
  track_added_to_playlist: [playlistKeys.details, playlistKeys.list],
  tracks_added_to_playlist: [playlistKeys.details, playlistKeys.list],
  track_removed_from_playlist: [playlistKeys.list],
  tracks_removed_from_playlist: [playlistKeys.list],
  playlist_created: [playlistKeys.list],
  playlist_deleted: [playlistKeys.list, playlistKeys.details],
  playlist_renamed: [],
  playlist_reordered: [],
};
