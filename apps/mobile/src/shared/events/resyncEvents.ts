import type { QueryClient } from '@tanstack/react-query';

import { libraryKeys, playlistKeys } from '@shared/lib/query-keys';

import type { ServerEventHandlers } from './eventPayload';
import { TRACK_CACHE_FAMILIES } from './trackCachePatch';

// The Track-bearing entries come from the one TRACK_CACHE_FAMILIES declaration;
// albums/artists/summary/list are the non-Track library families resync also drops.
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

function handleResync(queryClient: QueryClient): void {
  for (const queryKey of RESYNC_KEYS) {
    void queryClient.invalidateQueries({ queryKey });
  }
}

export const RESYNC_HANDLERS: ServerEventHandlers<'resync'> = {
  resync: handleResync,
};
