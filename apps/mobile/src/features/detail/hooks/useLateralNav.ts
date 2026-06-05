/**
 * useLateralNav — search-and-navigate to a related artist or album.
 *
 * AC#11-13: From track/album detail, tapping artist or album name searches for
 * that entity and navigates to its detail. Uses router.replace (not push) to
 * keep the back stack shallow — back always returns to Discover, not through
 * a chain of detail screens.
 */

import { useCallback, useState } from 'react';
import { useRouter } from 'expo-router';

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
        router.replace('/detail');
      } finally {
        setState('idle');
      }
    },
    [router, state],
  );

  return { navigateTo, state, error, clearError };
}
