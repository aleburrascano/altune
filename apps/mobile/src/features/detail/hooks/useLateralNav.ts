/**
 * useLateralNav — search-and-navigate to a related artist or album.
 *
 * AC#11-13: From track/album detail, tapping artist or album name searches for
 * that entity and navigates to its detail. Uses router.push to build a proper
 * back stack — back returns through the chain of detail screens.
 */

import { useCallback, useState } from 'react';
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

  const clearError = useCallback(() => setError(null), []);

  const navigateTo = useCallback(
    async (query: string, kind: DiscoveryKind): Promise<void> => {
      if (state === 'searching') {
        return;
      }

      setError(null);
      setState('searching');
      try {
        const response = await searchDiscovery({ q: query, kinds: [kind], limit: 1 });
        const result = response.results[0];

        if (result === undefined) {
          const kindLabel = kind === 'artist' ? 'Artist' : 'Album';
          setError(`${kindLabel} not found: "${query}"`);
          return;
        }

        setDetailHandoff(result);
        router.push(`/${tabRoot}/detail` as '/discover/detail');
      } finally {
        setState('idle');
      }
    },
    [router, state, tabRoot],
  );

  return { navigateTo, state, error, clearError };
}
