import type { TrackResponse } from '@shared/apiClient/trackResponse';
import type { PlaylistResponse } from '@shared/apiClient/playlistResponse';
import type { usePlayback } from '@shared/playback/usePlayback';
import type { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';
import { useAlbumsView, useArtistsView } from '@features/library/hooks/useGroupViews';
import type { useLibraryNavigation } from '@features/library/hooks/useLibraryNavigation';
import type { PlaylistActionsState } from '@features/library/hooks/usePlaylistActions';
import { usePlaylistsView } from '@features/library/hooks/usePlaylistsView';
import type { useRetryAcquisition } from '@features/library/hooks/useRetryAcquisition';
import type { Selection } from '@features/library/hooks/useSelection';
import { useTracksView } from '@features/library/hooks/useTracksView';
import type { ActiveView, LibraryChip, SortKey } from '@features/library/domain/libraryView';

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
