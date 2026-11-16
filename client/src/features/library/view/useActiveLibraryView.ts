import type { TrackResponse, PlaylistResponse } from '@shared/catalog';
import type { usePlayback, useQueuePlayback } from '@shared/playback';
import { showFailureAlert, type MenuAnchor } from '@shared/ui';
import { useAlbumsView, useArtistsView } from '@features/library/view/useGroupViews';
import type { useLibraryNavigation } from '@features/library/hooks/useLibraryNavigation';
import type { PlaylistActionsState } from '@features/library/view/usePlaylistActions';
import { usePlaylistsView } from '@features/library/view/usePlaylistsView';
import type { useRetryAcquisition } from '@features/library/view/useRetryAcquisition';
import type { Selection } from '@features/library/view/useSelection';
import { useTracksView } from '@features/library/view/useTracksView';
import type { ActiveView, LibraryChip, SortKey } from '@features/library/view/libraryView';

type ActiveLibraryViewDeps = {
  pl: PlaylistActionsState;
  navigation: ReturnType<typeof useLibraryNavigation>;
  selection: Selection;
  queue: ReturnType<typeof useQueuePlayback>;
  playback: ReturnType<typeof usePlayback>;
  retryMutation: ReturnType<typeof useRetryAcquisition>;
  onTrackMore: (track: TrackResponse, anchor: MenuAnchor) => void;
};

type ActiveLibraryView = {
  active: ActiveView;
  tracks: TrackResponse[];
  playlists: PlaylistResponse[];
  shuffleWholeLibrary: () => Promise<void>;
};

export function useActiveLibraryView(
  chip: LibraryChip,
  sortByChip: Record<LibraryChip, SortKey>,
  query: string,
  deps: ActiveLibraryViewDeps,
): ActiveLibraryView {
  const { pl, navigation, selection, queue, playback, retryMutation, onTrackMore } = deps;

  const tracksView = useTracksView({
    query,
    sort: sortByChip.tracks,
    isActive: chip === 'tracks',
    selection,
    queue,
    playback,
    retryMutation,
    onTrackPress: navigation.navigateToTrack,
    onTrackMore,
    showFailure: showFailureAlert,
  });

  const albumsView = useAlbumsView({
    query,
    sort: sortByChip.albums,
    isActive: chip === 'albums',
    onAlbumPress: navigation.navigateToAlbum,
  });

  const artistsView = useArtistsView({
    query,
    sort: sortByChip.artists,
    isActive: chip === 'artists',
    onArtistPress: navigation.navigateToArtist,
  });

  const playlistsView = usePlaylistsView({
    pl,
    sort: sortByChip.playlists,
    onPlaylistPress: navigation.navigateToPlaylist,
  });

  const viewByChip: Record<LibraryChip, ActiveView> = {
    playlists: playlistsView.view,
    tracks: tracksView.view,
    albums: albumsView,
    artists: artistsView,
  };

  return {
    active: viewByChip[chip],
    tracks: tracksView.tracks,
    playlists: playlistsView.playlists,
    shuffleWholeLibrary: tracksView.shuffleWholeLibrary,
  };
}
