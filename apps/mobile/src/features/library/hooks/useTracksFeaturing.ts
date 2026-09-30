import { useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';

import { listTracksFeaturing } from '@shared/api-client/tracks';
import type { FeaturedArtist } from '@shared/api-client/types';
import { libraryKeys } from '@shared/lib/query-keys';

import { failureLogFields } from '../failureLogFields';

const DIGITS = /^[0-9]+$/;

export function parseDeezerIdParam(raw: string | undefined): number | null {
  if (raw === undefined || !DIGITS.test(raw)) return null;
  const id = Number(raw);
  return Number.isSafeInteger(id) ? id : null;
}

export function parseStringParam(raw: string | string[] | undefined): string | null {
  const first = Array.isArray(raw) ? raw[0] : raw;
  return first === undefined || first.length === 0 ? null : first;
}

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

  useLoggedFeaturingQueryFailure(
    error,
    fa.mbid ?? (fa.deezer_id != null ? `dz:${fa.deezer_id}` : 'name'),
  );

  return { data, error, isLoading, isError, isRefetching, refetch };
}
