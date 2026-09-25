import { useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';

import { listTracksFeaturing } from '@shared/api-client/tracks';
import type { FeaturedArtist } from '@shared/api-client/types';
import { libraryKeys } from '@shared/lib/query-keys';

import { failureLogFields } from '../failureLogFields';

const DIGITS = /^[0-9]+$/;

// Route params arrive as strings from deep links, so anything that is not a plain
// non-negative integer (the only shape `String(deezer_id)` produces in-app) is null.
export function parseDeezerIdParam(raw: string | undefined): number | null {
  if (raw === undefined || !DIGITS.test(raw)) return null;
  const id = Number(raw);
  return Number.isSafeInteger(id) ? id : null;
}

/**
 * A failed featuring load renders the same generic copy whatever broke it, so without
 * this line the screen's main failure mode reaches production logs as nothing (#1706).
 * Redacted like #1703/#1704: `failureLogFields` keeps the caught error out, and `key`
 * carries only what the query was keyed by.
 */
function useLoggedFeaturingQueryFailure(error: Error | null, key: string): void {
  useEffect(() => {
    if (error === null) {
      return;
    }
    console.warn('[library] featuring query failed', { key, ...failureLogFields(error) });
  }, [error, key]);
}

export function useTracksFeaturing(input: FeaturedArtist) {
  const nonFiniteId = input.deezer_id != null && !Number.isFinite(input.deezer_id);
  const fa: FeaturedArtist = nonFiniteId ? { ...input, deezer_id: null } : input;
  const key = fa.mbid ?? (fa.deezer_id != null ? `dz:${fa.deezer_id}` : `name:${fa.name}`);
  const { data, error, isLoading, isError, isRefetching, refetch } = useQuery({
    queryKey: libraryKeys.featuring(key),
    queryFn: () => listTracksFeaturing(fa),
    enabled: fa.name.length > 0 || fa.mbid != null || fa.deezer_id != null,
    staleTime: 60_000,
  });

  useLoggedFeaturingQueryFailure(error, fa.mbid ?? (fa.deezer_id != null ? `dz:${fa.deezer_id}` : 'name'));

  return { data, isLoading, isError, isRefetching, refetch };
}
