import { useQuery, type QueryFunctionContext } from '@tanstack/react-query';
import type { DiscoveryResult, DiscoverySource } from '@shared/catalog';
import { detailKeys } from '@shared/query';

import {
  contentFailure,
  DETAIL_CONTENT_STALE_MS,
  type ContentFailure,
} from '@features/detail/core/contentStatus';
import { fetchTallyingOutcome } from '@features/detail/core/detailHealth';
import type { DetailPort } from '@features/detail/ports/detailPort';
import { useDetailPort } from '@features/detail/view/DetailPortProvider';
import { useContentFetchRetry } from '@features/detail/view/useContentFetchRetry';

type UseRelatedTracksParams = {
  sources: DiscoverySource[];
};

type UseRelatedTracksReturn = {
  relatedTracks: DiscoveryResult[];
  isLoading: boolean;
  isError: boolean;
  failure: ContentFailure | null;
};

function fetchRelated(
  port: DetailPort,
  scSource: DiscoverySource | null,
  signal: AbortSignal | undefined,
) {
  return fetchTallyingOutcome('related_tracks', () =>
    port.getRelatedTracks('soundcloud', scSource!.external_id, 20, signal),
  );
}

function relatedTracksOptions(port: DetailPort, scSource: DiscoverySource | null) {
  return {
    queryKey: detailKeys.relatedTracks(scSource?.external_id ?? ''),
    queryFn: ({ signal }: QueryFunctionContext) => fetchRelated(port, scSource, signal),
    enabled: scSource !== null,
    staleTime: DETAIL_CONTENT_STALE_MS,
  };
}

function useRelatedTracksQuery(scSource: DiscoverySource | null) {
  const port = useDetailPort();
  const retry = useContentFetchRetry();
  return useQuery({ ...relatedTracksOptions(port, scSource), retry });
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
  return relatedTracksResult(useRelatedTracksQuery(scSource));
}
