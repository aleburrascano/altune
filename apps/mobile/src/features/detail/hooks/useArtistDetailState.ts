import { useState, type Dispatch, type SetStateAction } from 'react';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { trackToDiscoveryResult } from '@shared/lib/track-to-discovery';

import { type ContentFailure } from '@/features/detail/domain/content-status';
import { type DetailRoute } from '@/features/detail/domain/detailNavigation';
import { useOpenDetail } from '@/features/detail/hooks/useOpenDetail';
import { type OwnedTrack } from '@/features/detail/domain/ownedTrack';
import { type OwnedSplit } from '@/features/detail/domain/owned-playback';
import { useArtistContent } from '@/features/detail/hooks/useArtistContent';
import { useArtistDiscovery } from '@/features/detail/hooks/useArtistDiscovery';
import { useLibraryAlbumsForArtist } from '@/features/detail/hooks/useLibraryAlbumsForArtist';
import { useLibraryTracksForArtist } from '@/features/detail/hooks/useLibraryTracks';
import { useSaveTrack } from '@/features/detail/hooks/useSaveTrack';
import { useOwnedPlayback } from '@/features/detail/hooks/useOwnedPlayback';

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

  const effectiveSources = hasSources
    ? artist.sources
    : discoverySearch.sources.length > 0
      ? discoverySearch.sources
      : artist.sources;
  const shouldFetchContent = effectiveSources.length > 0 && exploreExpanded;

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
    artistName: artist.title,
    enabled: shouldFetchContent,
  });

  const libraryTracksAsDiscovery = localTracks.map(trackToDiscoveryResult);
  const libraryAlbums = useLibraryAlbumsForArtist(artist.title, !hasSources);

  const { topTracks, isLoadingTracks, tracksFailure } = hasSources
    ? {
        topTracks: apiTopTracks,
        isLoadingTracks: apiContentLoading,
        tracksFailure: apiTracksFailure,
      }
    : {
        topTracks: libraryTracksAsDiscovery,
        isLoadingTracks: false,
        tracksFailure: null,
      };

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
      void discoverySearch.refetch();
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
