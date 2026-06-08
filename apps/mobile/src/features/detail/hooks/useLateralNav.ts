/**
 * useLateralNav — search-and-navigate to a related artist or album.
 *
 * AC#11-13: From track/album detail, tapping artist or album name searches for
 * that entity and navigates to its detail. Uses router.push to build a proper
 * back stack — back returns through the chain of detail screens.
 */

import { useCallback, useRef, useState } from 'react';
import { useRouter, useSegments } from 'expo-router';

import { searchDiscovery, type DiscoveryKind } from '@shared/api-client/discovery';
import { setDetailHandoff } from '@shared/lib/detail-handoff';

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
  const tabRoot = segments[1] === 'library' ? 'library' : 'discover';
  const [state, setState] = useState<LateralNavState>('idle');
  const [error, setError] = useState<string | null>(null);
  const searchingRef = useRef(false);

  const clearError = useCallback(() => setError(null), []);

  const navigateTo = useCallback(
    async (query: string, kind: DiscoveryKind): Promise<void> => {
      if (searchingRef.current) {
        return;
      }

      if (searchingRef.current) {
        return;
      }
      searchingRef.current = true;
      setError(null);
      setState('searching');
      try {
        const response = await searchDiscovery({ q: query, kinds: [kind], limit: 1, saveHistory: false });
        const result = response.results[0];

        if (result === undefined) {
          const kindLabel = kind === 'artist' ? 'Artist' : 'Album';
          setError(`${kindLabel} not found: "${query}"`);
          searchingRef.current = false;
          setState('idle');
          return;
        }

        setDetailHandoff(result);
        router.push(`/${tabRoot}/detail` as '/discover/detail');
        // Never reset searchingRef after a successful push — this screen
        // is now buried in the stack. The new detail screen gets its own
        // useLateralNav with a fresh ref. Resetting here causes duplicates.
      } catch {
        searchingRef.current = false;
        setState('idle');
      }
    },
    [router, tabRoot],
  );

  return { navigateTo, state, error, clearError };
}
