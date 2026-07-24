import { useQuery } from '@tanstack/react-query';

import { getRelatedTracks } from '@shared/api-client/enrichment';
import type { DiscoveryResult, DiscoverySource } from '@shared/api-client/discovery';

type UseRelatedTracksParams = {
  sources: DiscoverySource[];
  enabled?: boolean;
};

type UseRelatedTracksReturn = {
  relatedTracks: DiscoveryResult[];
  isLoading: boolean;
  isError: boolean;
};

export function useRelatedTracks({
  sources,
  enabled = true,
}: UseRelatedTracksParams): UseRelatedTracksReturn {
  const scSource = sources.find((s) => s.provider === 'soundcloud') ?? null;

  const { data, isLoading, isError } = useQuery({
    queryKey: ['related-tracks', scSource?.external_id ?? ''],
    queryFn: () => getRelatedTracks('soundcloud', scSource!.external_id, 20),
    enabled: enabled && scSource !== null,
    staleTime: 1000 * 60 * 30,
  });

  return {
    relatedTracks: data?.status === 'ok' ? data.items : [],
    isLoading,
    isError: isError || (data !== undefined && data.status !== 'ok'),
  };
}
