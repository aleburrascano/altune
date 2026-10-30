import {
  useQuery,
  useQueryClient,
  type NotifyOnChangeProps,
  type QueryClient,
  type QueryKey,
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
const REFRESH_ON_EVERY_POLL: NotifyOnChangeProps = ['data', 'dataUpdatedAt', 'isLoading', 'error'];
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

export type UnreleasedTracksState = {
  tracks: UnreleasedTrack[];
  isLoading: boolean;
  isFailed: boolean;
  retry: () => void;
};

function useUpdateCount(options: ReturnType<typeof unreleasedQueryOptions>): number {
  const queryClient = useQueryClient();
  return queryClient.getQueryState(options.queryKey)?.dataUpdateCount ?? 0;
}

function isBudgetSpent(response: UnreleasedTracksResponse | undefined, updateCount: number) {
  return response?.pending === true && !isStillComputing(response, updateCount);
}

function restartPollBudget(queryClient: QueryClient, queryKey: QueryKey): Promise<void> {
  return queryClient.resetQueries({ queryKey });
}

export function useUnreleasedTracks(artist: DiscoveryResult): UnreleasedTracksState {
  const port = useDetailPort();
  const queryClient = useQueryClient();
  const options = unreleasedQueryOptions(artist, port, useDetailFetchEnabled());
  const { data: unreleased, isLoading, isError } = useQuery(options);
  const updateCount = useUpdateCount(options);
  const isComputing = isStillComputing(unreleased, updateCount);
  const retry = () => void restartPollBudget(queryClient, options.queryKey);
  return {
    tracks: unreleased?.items ?? NO_TRACKS,
    isLoading: isLoading || isComputing,
    isFailed: isError || isBudgetSpent(unreleased, updateCount),
    retry,
  };
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

function fallbackState(track: UnreleasedTrack): UnreleasedCardState {
  return track.genius_url === null ? 'disabled' : 'genius';
}

function cardState(
  track: UnreleasedTrack,
  owned: OwnedTrack | null,
  save: SaveState,
): UnreleasedCardState {
  if (owned?.acquisitionStatus === 'ready') return 'play';
  return isAcquirable(track) ? save : fallbackState(track);
}

type CardActions = { genius: () => void; play: () => void; save: () => void };

function noAction(): void {}

function cardAction(state: UnreleasedCardState, actions: CardActions): () => void {
  if (state === 'genius' || state === 'play') return actions[state];
  return state === 'disabled' ? noAction : actions.save;
}

export function useUnreleasedCard({ track, artist, openGenius }: CardTarget): UnreleasedCard {
  const asTrack = unreleasedAsResult(track, artist);
  const owned = useOwnedUnreleased(asTrack);
  const save = useTrackSave(asTrack, owned);
  const play = usePlayOwned(asTrack, owned);
  const state = cardState(track, owned, save.state);
  return { state, onPress: cardAction(state, { genius: openGenius, play, save: save.onSave }) };
}
