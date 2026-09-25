import { useState, type Dispatch, type SetStateAction } from 'react';
import { useRouter } from 'expo-router';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { trackToDiscoveryResult } from '@shared/lib/track-to-discovery';

import { type ContentFailure } from '../content-status';
import { openDetail, type DetailRoute } from '../navigation';
import { type OwnedTrack } from './useOwnedTrack';
import { type OwnedSplit } from '../owned-playback';
import { useArtistContent } from './useArtistContent';
import { useArtistDiscovery } from './useArtistDiscovery';
import { useLibraryAlbumsForArtist } from './useLibraryAlbumsForArtist';
import { useLibraryTracksForArtist } from './useLibraryTracks';
import { useSaveTrack } from './useSaveTrack';
import { useOwnedPlayback } from './useOwnedPlayback';

export type ArtistDetailState = {
  hasSources: boolean;
  topTracks: DiscoveryResult[];
  isLoadingTracks: boolean;
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

export function useArtistDetailState(
  result: DiscoveryResult,
  detailRoute: DetailRoute,
): ArtistDetailState {
  const router = useRouter();
  const save = useSaveTrack();
  const hasSources = result.sources.length > 0;

  const localTracks = useLibraryTracksForArtist(result.title);
  const hasLibraryTracks = localTracks.length > 0;

  const [exploreExpanded, setExploreExpanded] = useState(hasSources);

  const discoverySearch = useArtistDiscovery({
    artistName: result.title,
    enabled: !hasSources && (exploreExpanded || hasLibraryTracks),
  });

  const effectiveSources = hasSources
    ? result.sources
    : discoverySearch.sources.length > 0
      ? discoverySearch.sources
      : result.sources;
  const shouldFetchContent = effectiveSources.length > 0 && exploreExpanded;

  const {
    topTracks: apiTopTracks,
    albums: apiAlbums,
    isLoadingTracks: apiLoadingTracks,
    isLoadingAlbums,
    isErrorAlbums,
    tracksFailure: apiTracksFailure,
    albumsFailure,
    refetchTracks,
    refetchAlbums,
  } = useArtistContent({
    sources: effectiveSources,
    artistName: result.title,
    enabled: shouldFetchContent,
  });

  const libraryTracksAsDiscovery = localTracks.map(trackToDiscoveryResult);
  const libraryAlbums = useLibraryAlbumsForArtist(result.title, !hasSources);

  const { topTracks, isLoadingTracks, tracksFailure } = hasSources
    ? {
        topTracks: apiTopTracks,
        isLoadingTracks: apiLoadingTracks,
        tracksFailure: apiTracksFailure,
      }
    : {
        topTracks: libraryTracksAsDiscovery,
        isLoadingTracks: false,
        // The top tracks are the library's own, which cannot fail.
        tracksFailure: null,
      };

  const onTrackPress = (track: DiscoveryResult): void => {
    openDetail(router, detailRoute, {
      ...track,
      image_url: track.image_url ?? result.image_url,
    });
  };

  const onAlbumPress = (album: DiscoveryResult): void => {
    openDetail(router, detailRoute, { ...album, subtitle: album.subtitle ?? result.title });
  };

  const { owned, playButton, onPlayOwned, ownedFor, onQuickSave } = useOwnedPlayback(
    topTracks,
    {
      title: result.title,
      image: result.image_url,
      enrich: (track) => ({
        ...track,
        subtitle: track.subtitle ?? result.title,
        image_url: track.image_url ?? result.image_url,
      }),
    },
    save,
  );

  return {
    hasSources,
    topTracks,
    isLoadingTracks,
    isErrorTracks: tracksFailure !== null,
    tracksFailure,
    refetchTracks,
    libraryAlbums,
    apiAlbums,
    isLoadingAlbums,
    isErrorAlbums,
    albumsFailure,
    refetchAlbums,
    exploreExpanded,
    setExploreExpanded,
    discoveryLoading: discoverySearch.isLoading,
    discoveryError: discoverySearch.isError,
    discoveryRefetch: () => {
      void discoverySearch.refetch();
      refetchAlbums();
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
