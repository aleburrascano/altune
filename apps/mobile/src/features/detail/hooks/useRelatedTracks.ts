import { useQuery } from '@tanstack/react-query';

import { getRelatedTracks } from '@shared/api-client/enrichment';
import type { DiscoveryResult, DiscoverySource } from '@shared/api-client/discovery';

import { contentFailure, type ContentFailure } from '../content-status';
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

export function useRelatedTracks({
  sources,
}: UseRelatedTracksParams): UseRelatedTracksReturn {
  const scSource = sources.find((s) => s.provider === 'soundcloud') ?? null;
  const retry = useContentFetchRetry();
  const isFetchEnabled = useDetailFetchEnabled();

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ['related-tracks', scSource?.external_id ?? ''],
    queryFn: ({ signal }) =>
      fetchTallyingOutcome('related_tracks', () =>
        getRelatedTracks('soundcloud', scSource!.external_id, 20, signal),
      ),
    enabled: isFetchEnabled && scSource !== null,
    staleTime: 1000 * 60 * 30,
    retry,
  });

  const failure = contentFailure(isError, error, data);

  return {
    relatedTracks: data?.status === 'ok' ? data.items : [],
    isLoading,
    isError: failure !== null,
    failure,
  };
}
