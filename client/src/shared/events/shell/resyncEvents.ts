import { libraryKeys, playlistKeys } from '@shared/query';

import type { EventCache } from '@shared/events/core/eventCache';
import type { ServerEventHandlers } from '@shared/events/core/serverEventTypes';
import { TRACK_CACHE_FAMILIES } from '@shared/events/core/trackCacheShapes';

const RESYNC_KEYS: readonly (readonly string[])[] = [
  TRACK_CACHE_FAMILIES.pagedLibrary.prefix,
  TRACK_CACHE_FAMILIES.lookup.prefix,
  libraryKeys.albumsPrefix,
  libraryKeys.artistsPrefix,
  libraryKeys.summary,
  TRACK_CACHE_FAMILIES.featuring.prefix,
  playlistKeys.list,
  TRACK_CACHE_FAMILIES.playlistDetails.prefix,
];

function handleResync(cache: EventCache): void {
  for (const queryKey of RESYNC_KEYS) {
    void cache.invalidateQueries({ queryKey });
  }
}

export const RESYNC_HANDLERS: ServerEventHandlers<'resync'> = {
  resync: handleResync,
};
