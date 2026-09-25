import type { TrackResponse } from '@shared/api-client/types';
import { isCurrentlyPlaying } from '@shared/playback/isCurrentlyPlaying';
import { buildPlayableQueue } from '@shared/playback/playFromList';
import type { usePlayback } from '@shared/playback/usePlayback';
import type { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';

import { useLibraryTracks } from './useLibraryTracks';
import type { useRetryAcquisition } from './useRetryAcquisition';
import type { Selection } from './useSelection';
import type { ActiveView } from '../activeView';
import type { ListRefresh } from '../refresh';
import { TRACK_SORT_OPTIONS, type SortKey } from '../sort';
import { TracksList } from '../ui/TracksList';

export type TracksViewDeps = {
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

export type TracksView = {
  view: ActiveView;
  tracks: TrackResponse[];
  shuffleWholeLibrary: () => Promise<void>;
};

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

  // loadAll, not the rendered pages: the queue must span the whole library, which
  // paginates beyond what is on screen (#30).
  const playWholeLibraryFrom = async (track: TrackResponse): Promise<void> => {
    const all = await tracksState.loadAll();
    const { playable, startIndex } = buildPlayableQueue(all, track.id);
    queue.playFromList(playable, startIndex, { kind: 'library' });
  };

  const shuffleWholeLibrary = async (): Promise<void> => {
    const all = await tracksState.loadAll();
    const { playable } = buildPlayableQueue(all, '');
    queue.shuffleFromList(playable, { kind: 'library' });
  };

  const refresh: ListRefresh = {
    refreshing: tracksState.isRefetching,
    onRefresh: tracksState.refetch,
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
          onEndReached={tracksState.onEndReached}
          isFetchingNextPage={tracksState.isFetchingNextPage}
          nextPageFailed={tracksState.nextPageFailed}
          onRetryNextPage={tracksState.onRetryNextPage}
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
