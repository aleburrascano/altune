import { useQuery } from '@tanstack/react-query';

import type { DiscoveryResult, DiscoverySource } from '@shared/api-client/discovery';

import {
  contentFailure,
  DETAIL_CONTENT_STALE_MS,
  type ContentFailure,
} from '@/features/detail/domain/contentStatus';
import { fetchTallyingOutcome } from '@/features/detail/domain/detailHealth';
import type { DetailPort } from '@/features/detail/ports/detailPort';
import { useDetailPort } from '@/features/detail/ports/DetailPortProvider';
import {
  useDetailFetchEnabled,
  useContentFetchRetry,
} from '@/features/detail/hooks/useDetailFetchGate';

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

function useRelatedTracksQuery(scSource: DiscoverySource | null, isFetchEnabled: boolean) {
  const port = useDetailPort();
  const retry = useContentFetchRetry();
  return useQuery({
    queryKey: ['related-tracks', scSource?.external_id ?? ''],
    queryFn: ({ signal }) => fetchRelated(port, scSource, signal),
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
