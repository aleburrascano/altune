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

      const allowAnotherAttempt = () => {
        searchingRef.current = false;
        setState('idle');
      };

      try {
        const results = await queryClient.fetchQuery({
          ...resolveEntityQuery(kind, query, 1),
          retry: false,
        });
        const result = results[0];

        if (result === undefined) {
          const kindLabel = kind === 'artist' ? 'Artist' : 'Album';
          setError(`${kindLabel} not found: "${query}"`);
          allowAnotherAttempt();
          return;
        }

        openDetail(router, detailRouteFor(tabRoot), result);
      } catch {
        allowAnotherAttempt();
      }
    },
    [router, tabRoot, queryClient],
  );

  return { navigateTo, state, error, clearError };
}
