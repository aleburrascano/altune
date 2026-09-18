import type { useRouter } from 'expo-router';

import type { TrackResponse, PlaylistResponse } from '@shared/api-client/types';
import type { usePlayback } from '@shared/playback/usePlayback';
import type { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';

import { useAlbumsView } from './useAlbumsView';
import { useArtistsView } from './useArtistsView';
import type { useLibraryNavigation } from './useLibraryNavigation';
import type { PlaylistActionsState } from './usePlaylistActions';
import { usePlaylistsView } from './usePlaylistsView';
import type { useRetryAcquisition } from './useRetryAcquisition';
import type { Selection } from './useSelection';
import { useTracksView } from './useTracksView';
import type { ActiveView } from '../activeView';
import type { SortKey } from '../sort';
import type { LibraryChip } from '../ui/LibraryChips';

export type ActiveLibraryViewDeps = {
  pl: PlaylistActionsState;
  router: ReturnType<typeof useRouter>;
  navigation: ReturnType<typeof useLibraryNavigation>;
  selection: Selection;
  queue: ReturnType<typeof useQueuePlayback>;
  playback: ReturnType<typeof usePlayback>;
  retryMutation: ReturnType<typeof useRetryAcquisition>;
  onTrackMore: (track: TrackResponse, anchor: MenuAnchor) => void;
};

export type ActiveLibraryView = {
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
  const { pl, router, navigation, selection, queue, playback, retryMutation, onTrackMore } = deps;

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
    onPlaylistPress: (playlist) => router.push(`/library/playlist/${playlist.id}`),
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
