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
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { detailKeys } from '@shared/query/queryKeys';

import { DETAIL_CONTENT_STALE_MS } from '@features/detail/domain/contentStatus';
import {
  ownedFromTrackInfo,
  toPlaybackQueue,
  type OwnedTrack,
} from '@features/detail/domain/ownedTrack';
import type { SaveState } from '@features/detail/domain/saveTrack';
import { trackExtras } from '@features/detail/domain/trackExtras';
import { isAcquirable, unreleasedAsResult } from '@features/detail/domain/unreleasedTrack';
import type { DetailPort } from '@features/detail/ports/detailPort';
import { useDetailPort } from '@features/detail/ports/DetailPortProvider';
import { useDetailFetchEnabled } from '@features/detail/hooks/useDetailFetchGate';
import { useResolvedOwnedTrack } from '@features/detail/hooks/useOwnedTrack';
import { useTrackSave } from '@features/detail/hooks/useSaveTrack';

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

export type UnreleasedCardState = SaveState | 'genius' | 'play';

export type UnreleasedCard = { state: UnreleasedCardState; onPress: () => void };

type CardTarget = { track: UnreleasedTrack; artist: DiscoveryResult; openGenius: () => void };

const LIBRARY_QUEUE = { kind: 'library' } as const;

function useOwnedUnreleased(asTrack: DiscoveryResult): OwnedTrack | null {
  const stamped = ownedFromTrackInfo(trackExtras(asTrack.extras));
  return useResolvedOwnedTrack(stamped, { title: asTrack.title, artist: asTrack.subtitle });
}

function usePlayOwned(asTrack: DiscoveryResult, owned: OwnedTrack | null): () => void {
  const queue = useQueuePlayback();
  return () => {
    if (owned === null) return;
    const playable = toPlaybackQueue([{ owned, discovery: asTrack }], asTrack.subtitle, null);
    queue.playFromList(playable, 0, LIBRARY_QUEUE);
  };
}

function cardState(
  acquirable: boolean,
  owned: OwnedTrack | null,
  save: SaveState,
): UnreleasedCardState {
  if (owned?.acquisitionStatus === 'ready') return 'play';
  return acquirable ? save : 'genius';
}

export function useUnreleasedCard({ track, artist, openGenius }: CardTarget): UnreleasedCard {
  const asTrack = unreleasedAsResult(track, artist);
  const owned = useOwnedUnreleased(asTrack);
  const save = useTrackSave(asTrack, owned);
  const play = usePlayOwned(asTrack, owned);
  const state = cardState(isAcquirable(track), owned, save.state);
  const actions = { genius: openGenius, play };
  return { state, onPress: state === 'genius' || state === 'play' ? actions[state] : save.onSave };
}
