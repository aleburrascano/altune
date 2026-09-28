import { useQuery } from '@tanstack/react-query';

import { getRelatedTracks } from '@shared/api-client/enrichment';
import type { DiscoveryResult, DiscoverySource } from '@shared/api-client/discovery';

import { contentFailure, DETAIL_CONTENT_STALE_MS, type ContentFailure } from '../content-status';
import { fetchTallyingOutcome } from '../detailHealth';
import { useDetailFetchEnabled } from './detailFetchGate';
import { useContentFetchRetry } from './useContentFetchRetry';

type UseRelatedTracksParams = {
  sources: DiscoverySource[];
};

type UseRelatedTracksReturn = {
  relatedTracks: DiscoveryResult[];
  isLoading: boolean;
  isError: boolean;
  failure: ContentFailure | null;
};

function fetchRelated(scSource: DiscoverySource | null, signal: AbortSignal | undefined) {
  return fetchTallyingOutcome('related_tracks', () =>
    getRelatedTracks('soundcloud', scSource!.external_id, 20, signal),
  );
}

function useRelatedTracksQuery(scSource: DiscoverySource | null, isFetchEnabled: boolean) {
  const retry = useContentFetchRetry();
  return useQuery({
    queryKey: ['related-tracks', scSource?.external_id ?? ''],
    queryFn: ({ signal }) => fetchRelated(scSource, signal),
    enabled: isFetchEnabled && scSource !== null,
    staleTime: DETAIL_CONTENT_STALE_MS,
    retry,
  });
}

type RelatedTracksQuery = ReturnType<typeof useRelatedTracksQuery>;

function relatedTracksItems(query: RelatedTracksQuery): DiscoveryResult[] {
  return query.data?.status === 'ok' ? query.data.items : [];
}

function relatedTracksResult(query: RelatedTracksQuery): UseRelatedTracksReturn {
  const failure = contentFailure(query.isError, query.error, query.data);
  return {
    relatedTracks: relatedTracksItems(query),
    isLoading: query.isLoading,
    isError: failure !== null,
    failure,
  };
}

export function useRelatedTracks({ sources }: UseRelatedTracksParams): UseRelatedTracksReturn {
  const scSource = sources.find((s) => s.provider === 'soundcloud') ?? null;
  const isFetchEnabled = useDetailFetchEnabled();
  return relatedTracksResult(useRelatedTracksQuery(scSource, isFetchEnabled));
}
