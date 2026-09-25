import { useEffect, useMemo } from 'react';
import type { InfiniteData } from '@tanstack/react-query';

import type { DiscoverySearchResponse } from '@shared/api-client/discovery';

import { useGatedDiscoverCall } from './discoverFetchGate';

type SearchPageParam = { offset: number; searchId: string | undefined };
type SearchPages = InfiniteData<DiscoverySearchResponse, unknown>;

function slateMismatch(page: DiscoverySearchResponse, pageParam: unknown): boolean {
  const sentId = (pageParam as SearchPageParam | undefined)?.searchId;
  return sentId !== undefined && page.search_id !== sentId;
}

function firstSlateMismatch(source: SearchPages | undefined): number | undefined {
  if (source === undefined) return undefined;
  const index = source.pages.findIndex((page, i) => slateMismatch(page, source.pageParams[i]));
  return index === -1 ? undefined : index;
}

function useRestartWhenExpired(expiredAt: number | undefined, restart: () => void): void {
  useEffect(() => {
    if (expiredAt !== undefined) restart();
  }, [expiredAt, restart]);
}

export function useRestartOnExpiredSlate(
  source: SearchPages | undefined,
  restart: () => unknown,
): DiscoverySearchResponse[] | undefined {
  const expiredAt = firstSlateMismatch(source);
  useRestartWhenExpired(expiredAt, useGatedDiscoverCall(restart));
  return useMemo(() => source?.pages.slice(0, expiredAt ?? source.pages.length), [source, expiredAt]);
}
