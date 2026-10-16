import {
  useQuery,
  useQueryClient,
  type NotifyOnChangeProps,
  type Query,
  type QueryFunctionContext,
} from '@tanstack/react-query';

import type { DiscoveryResult } from '@shared/apiClient/discovery';
import type { UnreleasedTrack, UnreleasedTracksResponse } from '@shared/apiClient/enrichment';
import { sourceIdList } from '@shared/apiClient/ids';
import { detailKeys } from '@shared/query/queryKeys';

import { DETAIL_CONTENT_STALE_MS } from '@features/detail/domain/contentStatus';
import type { DetailPort } from '@features/detail/ports/detailPort';
import { useDetailPort } from '@features/detail/ports/DetailPortProvider';
import { useDetailFetchEnabled } from '@features/detail/hooks/useDetailFetchGate';

const NO_TRACKS: UnreleasedTrack[] = [];
const REFRESH_ON_EVERY_POLL: NotifyOnChangeProps = ['data', 'dataUpdatedAt', 'isLoading'];
const PENDING_POLL_MS = 10_000;
const PENDING_POLL_BUDGET_MS = 180_000;
const MAX_PENDING_POLLS = PENDING_POLL_BUDGET_MS / PENDING_POLL_MS;

function isStillComputing(response: UnreleasedTracksResponse | undefined, updateCount: number) {
  return response?.pending === true && updateCount <= MAX_PENDING_POLLS;
}

function pendingPollInterval(query: Query<UnreleasedTracksResponse>): number | false {
  const isComputing = isStillComputing(query.state.data, query.state.dataUpdateCount);
  return isComputing ? PENDING_POLL_MS : false;
}

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
    refetchInterval: pendingPollInterval,
    notifyOnChangeProps: REFRESH_ON_EVERY_POLL,
  };
}

export type UnreleasedTracksState = { tracks: UnreleasedTrack[]; isLoading: boolean };

function useUpdateCount(options: ReturnType<typeof unreleasedQueryOptions>): number {
  const queryClient = useQueryClient();
  return queryClient.getQueryState(options.queryKey)?.dataUpdateCount ?? 0;
}

export function useUnreleasedTracks(artist: DiscoveryResult): UnreleasedTracksState {
  const port = useDetailPort();
  const options = unreleasedQueryOptions(artist, port, useDetailFetchEnabled());
  const { data: unreleased, isLoading } = useQuery(options);
  const isComputing = isStillComputing(unreleased, useUpdateCount(options));
  return { tracks: unreleased?.items ?? NO_TRACKS, isLoading: isLoading || isComputing };
}
