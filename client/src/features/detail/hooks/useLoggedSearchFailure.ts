import { useEffect } from 'react';

import type { DiscoveryKind } from '@shared/api-client/discovery';

type SearchedEntity = {
  kind: DiscoveryKind;
  title: string;
  artist: string | null;
};

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
