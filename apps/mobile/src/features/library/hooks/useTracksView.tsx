import { useRef, useState } from 'react';

import type { TrackResponse } from '@shared/api-client/types';
import { isCurrentlyPlaying } from '@shared/playback/isCurrentlyPlaying';
import { buildPlayableQueue } from '@shared/playback/playFromList';
import type { usePlayback } from '@shared/playback/usePlayback';
import type { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';

import { alertLibraryFailure } from '../libraryFailureAlert';
import { useLibraryTracks } from './useLibraryTracks';
import type { useRetryAcquisition } from './useRetryAcquisition';
import type { Selection } from './useSelection';
import type { ActiveView } from '../activeView';
import type { ListPaging, ListRefresh } from '../refresh';
import { TRACK_SORT_OPTIONS, type SortKey } from '../sort';
import { TracksList } from '../ui/TracksList';

type TracksViewDeps = {
  query: string;
  sort: SortKey;
  isActive: boolean;
  selection: Selection;
  queue: ReturnType<typeof useQueuePlayback>;
  playback: ReturnType<typeof usePlayback>;
  retryMutation: ReturnType<typeof useRetryAcquisition>;
  onTrackPress: (track: TrackResponse) => void;
  onTrackMore: (track: TrackResponse, anchor: MenuAnchor) => void;
};

type TracksView = {
  view: ActiveView;
  tracks: TrackResponse[];
  shuffleWholeLibrary: () => Promise<void>;
};

async function runOnce(
  inFlight: { current: boolean },
  setRunning: (running: boolean) => void,
  action: () => Promise<void>,
): Promise<void> {
  if (inFlight.current) return;
  setRunning(true);
  await action().finally(() => setRunning(false));
}

function useExclusive() {
  const inFlight = useRef(false);
  const [busy, setBusy] = useState(false);
  const setRunning = (running: boolean) => {
    inFlight.current = running;
    setBusy(running);
  };
  return { busy, run: (action: () => Promise<void>) => runOnce(inFlight, setRunning, action) };
}

export function useTracksView({
  query,
  sort,
  isActive,
  selection,
  queue,
  playback,
  retryMutation,
  onTrackPress,
  onTrackMore,
}: TracksViewDeps): TracksView {
  const tracksState = useLibraryTracks(query, sort, isActive);

  const { busy: wholeLibraryBusy, run: exclusively } = useExclusive();

  const loadAllOrWarn = () =>
    tracksState.loadAll((failure, loaded) =>
      alertLibraryFailure(
        'Playing loaded tracks only',
        `Could not load your whole library. Playing the ${loaded} tracks loaded so far.`,
        failure,
      ),
    );

  const playWholeLibraryFrom = (track: TrackResponse): Promise<void> =>
    exclusively(async () => {
      const whole = await loadAllOrWarn();
      const all = whole.some((t) => t.id === track.id) ? whole : tracksState.tracks;
      const { playable, startIndex } = buildPlayableQueue(all, track.id);
      queue.playFromList(playable, startIndex, { kind: 'library' });
    });

  const shuffleWholeLibrary = (): Promise<void> =>
    exclusively(async () => {
      const all = await loadAllOrWarn();
      const { playable } = buildPlayableQueue(all, '');
      queue.shuffleFromList(playable, { kind: 'library' });
    });

  const refresh: ListRefresh = {
    refreshing: tracksState.isRefetching,
    onRefresh: tracksState.refetch,
  };

  const paging: ListPaging = {
    onEndReached: tracksState.onEndReached,
    isFetchingNextPage: tracksState.isFetchingNextPage,
    nextPageFailed: tracksState.nextPageFailed,
    onRetryNextPage: tracksState.onRetryNextPage,
  };

  return {
    tracks: tracksState.tracks,
    shuffleWholeLibrary,
    view: {
      count: tracksState.tracks.length === 0 ? 0 : tracksState.total,
      noun: 'track',
      options: TRACK_SORT_OPTIONS,
      isLoading: tracksState.isLoading,
      error: tracksState.tracks.length === 0 ? tracksState.error : null,
      onRetry: tracksState.refetch,
      content: (
        <TracksList
          tracks={tracksState.tracks}
          emptyLabel={'No tracks yet'}
          refresh={refresh}
          paging={paging}
          shuffleBusy={wholeLibraryBusy}
          onShuffleAll={() => void shuffleWholeLibrary()}
          onPlay={(track) => void playWholeLibraryFrom(track)}
          onPress={onTrackPress}
          onMore={onTrackMore}
          onRetry={(track) => retryMutation.mutate(track.id)}
          isRetrying={retryMutation.isInFlight}
          isPlaying={(id) => isCurrentlyPlaying(playback, { kind: 'library', trackId: id })}
          selection={selection}
        />
      ),
    },
  };
}
