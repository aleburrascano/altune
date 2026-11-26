import { useState, type Dispatch, type SetStateAction } from 'react';
import type { DiscoveryResult } from '@shared/catalog';
import { trackToDiscoveryResult } from '@shared/catalog';
import { type ContentFailure } from '@features/detail/core/contentStatus';
import { type DetailRoute } from '@features/detail/core/detailNavigation';
import { useOpenDetail } from '@features/detail/view/useDetailNavigation';
import { type OwnedTrack, type OwnedSplit } from '@features/detail/core/ownedTrack';
import { useArtistContent } from '@features/detail/view/useArtistContent';
import { useArtistDiscovery } from '@features/detail/view/useEntityDiscovery';
import {
  useLibraryAlbumsForArtist,
  useLibraryTracksForArtist,
} from '@features/detail/view/useLibraryMatches';
import { useSaveTrack } from '@features/detail/view/useSaveTrack';
import { useOwnedPlayback } from '@features/detail/view/useOwnedPlayback';

export type ArtistDetailState = {
  hasSources: boolean;
  topTracks: DiscoveryResult[];
  isLoadingTracks: boolean;
  libraryTracksComplete: boolean;
  isErrorTracks: boolean;
  tracksFailure: ContentFailure | null;
  refetchTracks: () => void;
  libraryAlbums: DiscoveryResult[];
  apiAlbums: DiscoveryResult[];
  isLoadingAlbums: boolean;
  isErrorAlbums: boolean;
  albumsFailure: ContentFailure | null;
  refetchAlbums: () => void;
  exploreExpanded: boolean;
  setExploreExpanded: Dispatch<SetStateAction<boolean>>;
  discoveryLoading: boolean;
  discoveryError: boolean;
  discoveryRefetch: () => void;
  onTrackPress: (track: DiscoveryResult) => void;
  onAlbumPress: (album: DiscoveryResult) => void;
  onQuickSave: (track: DiscoveryResult) => void;
  ownedFor: (track: DiscoveryResult) => OwnedTrack | null;
  owned: OwnedSplit;
  playButton: { label: string; disabled: boolean };
  onPlayOwned: () => void;
};

function contentSourcesFor(
  artist: DiscoveryResult,
  hasSources: boolean,
  discoverySources: DiscoveryResult['sources'],
): DiscoveryResult['sources'] {
  if (hasSources) return artist.sources;
  return discoverySources.length > 0 ? discoverySources : artist.sources;
}

type ShownTopTracks = Pick<ArtistDetailState, 'topTracks' | 'isLoadingTracks' | 'tracksFailure'>;

function shownTopTracks(
  hasSources: boolean,
  api: ShownTopTracks,
  libraryTracks: DiscoveryResult[],
): ShownTopTracks {
  return hasSources
    ? api
    : { topTracks: libraryTracks, isLoadingTracks: false, tracksFailure: null };
}

export function useArtistDetailState(
  artist: DiscoveryResult,
  detailRoute: DetailRoute,
): ArtistDetailState {
  const openDetail = useOpenDetail(detailRoute);
  const save = useSaveTrack();
  const hasSources = artist.sources.length > 0;

  const localTracks = useLibraryTracksForArtist(artist.title);
  const hasLibraryTracks = localTracks.length > 0;

  const [exploreExpanded, setExploreExpanded] = useState(hasSources);

  const discoverySearch = useArtistDiscovery({
    artistName: artist.title,
    enabled: !hasSources && (exploreExpanded || hasLibraryTracks),
  });

  const effectiveSources = contentSourcesFor(artist, hasSources, discoverySearch.sources);
  const shouldFetchContent = effectiveSources.length > 0 && (hasSources || exploreExpanded);

  const {
    topTracks: apiTopTracks,
    albums: apiAlbums,
    isLoading: apiContentLoading,
    isErrorAlbums,
    tracksFailure: apiTracksFailure,
    albumsFailure,
    refetch: refetchContent,
  } = useArtistContent({
    sources: effectiveSources,
    enabled: shouldFetchContent,
  });

  const libraryTracksAsDiscovery = localTracks.map(trackToDiscoveryResult);
  const libraryAlbums = useLibraryAlbumsForArtist(artist.title, !hasSources);

  const { topTracks, isLoadingTracks, tracksFailure } = shownTopTracks(
    hasSources,
    {
      topTracks: apiTopTracks,
      isLoadingTracks: apiContentLoading,
      tracksFailure: apiTracksFailure,
    },
    libraryTracksAsDiscovery,
  );

  const onTrackPress = (track: DiscoveryResult): void => {
    openDetail({
      ...track,
      image_url: track.image_url ?? artist.image_url,
    });
  };

  const onAlbumPress = (album: DiscoveryResult): void => {
    openDetail({ ...album, subtitle: album.subtitle ?? artist.title });
  };

  const { owned, playButton, onPlayOwned, ownedFor, onQuickSave } = useOwnedPlayback(
    topTracks,
    {
      title: artist.title,
      image: artist.image_url,
      enrich: (track) => ({
        ...track,
        subtitle: track.subtitle ?? artist.title,
        image_url: track.image_url ?? artist.image_url,
      }),
      retryEntryPoint: 'artist_row',
    },
    save,
  );

  return {
    hasSources,
    topTracks,
    isLoadingTracks,
    libraryTracksComplete: localTracks.complete !== false,
    isErrorTracks: tracksFailure !== null,
    tracksFailure,
    refetchTracks: refetchContent,
    libraryAlbums,
    apiAlbums,
    isLoadingAlbums: apiContentLoading,
    isErrorAlbums,
    albumsFailure,
    refetchAlbums: refetchContent,
    exploreExpanded,
    setExploreExpanded,
    discoveryLoading: discoverySearch.isLoading,
    discoveryError: discoverySearch.isError,
    discoveryRefetch: () => {
      discoverySearch.refetch();
      refetchContent();
    },
    onTrackPress,
    onAlbumPress,
    onQuickSave,
    ownedFor,
    owned,
    playButton,
    onPlayOwned,
  };
}
