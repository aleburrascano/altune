import { useEffect } from 'react';

import type { DiscoveryKind } from '@shared/api-client/discovery';

/** The entity a discovery search was looking for, in the caller's own terms. */
type SearchedEntity = {
  kind: DiscoveryKind;
  title: string;
  artist: string | null;
};

/**
 * A failed search yields no result — the same shape as an entity that simply is
 * not out there — and the request's own log strips the query string, so without
 * this line the two are indistinguishable and a broken search names no entity
 * anyone can look up afterwards.
 */
export function useLoggedSearchFailure(error: Error | null, searched: SearchedEntity): void {
  const { kind, title, artist } = searched;

  useEffect(() => {
    if (error === null) {
      return;
    }
    console.warn('[detail] discovery search failed', {
      kind,
      title,
      artist,
      error: error.message,
    });
  }, [error, kind, title, artist]);
}
