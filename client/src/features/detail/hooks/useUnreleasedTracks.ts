import { useQuery, type QueryFunctionContext } from '@tanstack/react-query';

import type { DiscoveryResult } from '@shared/apiClient/discovery';
import type { UnreleasedTrack } from '@shared/apiClient/enrichment';
import { sourceIdList } from '@shared/apiClient/ids';
import { detailKeys } from '@shared/query/queryKeys';

import { DETAIL_CONTENT_STALE_MS } from '@features/detail/domain/contentStatus';
import type { DetailPort } from '@features/detail/ports/detailPort';
import { useDetailPort } from '@features/detail/ports/DetailPortProvider';
import { useDetailFetchEnabled } from '@features/detail/hooks/useDetailFetchGate';

const NO_TRACKS: UnreleasedTrack[] = [];

function fetchUnreleased(port: DetailPort, artist: DiscoveryResult, signal: AbortSignal) {
  const [seed] = artist.sources;
  const request = { name: artist.title, knownIds: artist.sources };
  return port.getUnreleasedTracks(seed!.provider, seed!.external_id, request, signal);
}

function unreleasedQueryOptions(artist: DiscoveryResult, port: DetailPort, isEnabled: boolean) {
  return {
    queryKey: detailKeys.unreleasedTracks(sourceIdList(artist.sources)),
    queryFn: ({ signal }: QueryFunctionContext) => fetchUnreleased(port, artist, signal),
    enabled: isEnabled && artist.sources.length > 0,
    staleTime: DETAIL_CONTENT_STALE_MS,
    retry: false,
  };
}

export function useUnreleasedTracks(artist: DiscoveryResult): UnreleasedTrack[] {
  const port = useDetailPort();
  const isFetchEnabled = useDetailFetchEnabled();
  const { data: unreleased } = useQuery(unreleasedQueryOptions(artist, port, isFetchEnabled));
  return unreleased?.items ?? NO_TRACKS;
}
