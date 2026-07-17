/**
 * useLateralNav — search-and-navigate to a related artist or album.
 *
 * AC#11-13: From track/album detail, tapping artist or album name searches for
 * that entity and navigates to its detail. Uses router.push to build a proper
 * back stack — back returns through the chain of detail screens. The lookup
 * goes through the shared resolve-entity cache, so the landed screen's own
 * name-resolution (useArtistDiscovery / useAlbumDiscovery) reuses this fetch
 * instead of re-hitting the backend.
 */

import { useCallback, useRef, useState } from 'react';
import { useRouter, useSegments } from 'expo-router';
import { useQueryClient } from '@tanstack/react-query';

import type { DiscoveryKind } from '@shared/api-client/discovery';

import { detailRouteFor, openDetail, tabRootFromSegments } from '../navigation';
import { resolveEntityQuery } from '../resolve-entity-query';

type LateralNavState = 'idle' | 'searching';

type UseLateralNavReturn = {
  navigateTo: (query: string, kind: DiscoveryKind) => Promise<void>;
  state: LateralNavState;
  error: string | null;
  clearError: () => void;
};

export function useLateralNav(): UseLateralNavReturn {
  const router = useRouter();
  const segments = useSegments();
  const queryClient = useQueryClient();
  const tabRoot = tabRootFromSegments(segments);
  const [state, setState] = useState<LateralNavState>('idle');
  const [error, setError] = useState<string | null>(null);
  const searchingRef = useRef(false);

  const clearError = useCallback(() => setError(null), []);

  const navigateTo = useCallback(
    async (query: string, kind: DiscoveryKind): Promise<void> => {
      if (searchingRef.current) {
        return;
      }
      searchingRef.current = true;
      setError(null);
      setState('searching');
      try {
        // retry: false preserves the pre-cache behavior — one attempt, fail fast.
        const results = await queryClient.fetchQuery({
          ...resolveEntityQuery(kind, query, 1),
          retry: false,
        });
        const result = results[0];

        if (result === undefined) {
          const kindLabel = kind === 'artist' ? 'Artist' : 'Album';
          setError(`${kindLabel} not found: "${query}"`);
          searchingRef.current = false;
          setState('idle');
          return;
        }

        openDetail(router, detailRouteFor(tabRoot), result);
        // Never reset searchingRef after a successful push — this screen
        // is now buried in the stack. The new detail screen gets its own
        // useLateralNav with a fresh ref. Resetting here causes duplicates.
      } catch {
        searchingRef.current = false;
        setState('idle');
      }
    },
    [router, tabRoot, queryClient],
  );

  return { navigateTo, state, error, clearError };
}
