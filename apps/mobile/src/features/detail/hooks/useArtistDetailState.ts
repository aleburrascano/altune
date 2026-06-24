/**
 * useArtistDetailState — the data + behaviour behind ArtistDetailBody.
 *
 * Owns the source resolution, the library/discovery content fetch, the
 * explore-discography expansion, and the navigation handlers, leaving the
 * component a presentational shell. Lifted out of ArtistDetailBody to drop
 * its cognitive complexity and isolate the union/merge logic for testing.
 */
import { useState, type Dispatch, type SetStateAction } from 'react';
import { useRouter } from 'expo-router';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { setDetailHandoff } from '@shared/lib/detail-handoff';
import { deriveAlbums } from '@shared/lib/derive-library-groups';

import { useArtistContent } from './useArtistContent';
import { useArtistDiscovery } from './useArtistDiscovery';
import { useLibraryTracksForArtist, libraryTrackToDiscoveryResult } from './useLibraryTracks';

export type ArtistDetailState = {
  hasSources: boolean;
  topTracks: DiscoveryResult[];
  isLoadingTracks: boolean;
  isErrorTracks: boolean;
  refetchTracks: () => void;
  libraryAlbums: DiscoveryResult[];
  apiAlbums: DiscoveryResult[];
  isLoadingAlbums: boolean;
  isErrorAlbums: boolean;
  refetchAlbums: () => void;
  exploreExpanded: boolean;
  setExploreExpanded: Dispatch<SetStateAction<boolean>>;
  discoveryLoading: boolean;
  discoveryError: boolean;
  onTrackPress: (track: DiscoveryResult) => void;
  onAlbumPress: (album: DiscoveryResult) => void;
};

export function useArtistDetailState(
  result: DiscoveryResult,
  detailRoute: string,
  isFromLibrary?: boolean,
): ArtistDetailState {
  const router = useRouter();
  const hasSources = !isFromLibrary && result.sources.length > 0;

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
    isErrorTracks: apiErrorTracks,
    isErrorAlbums,
    refetchTracks,
    refetchAlbums,
  } = useArtistContent({
    sources: effectiveSources,
    artistName: result.title,
    enabled: shouldFetchContent,
  });

  const libraryTracksAsDiscovery = localTracks.map(libraryTrackToDiscoveryResult);
  const libraryAlbums: DiscoveryResult[] = deriveAlbums(localTracks).map((g) => ({
    kind: 'album' as const,
    title: g.album,
    subtitle: g.artist,
    image_url: g.artworkUrl,
    confidence: 'high' as const,
    sources: [],
    extras: {
      track_count: g.trackCount,
      ...(g.year != null ? { year: g.year } : {}),
    },
  }));

  const topTracks = hasSources ? apiTopTracks : libraryTracksAsDiscovery;
  const isLoadingTracks = hasSources ? apiLoadingTracks : false;
  const isErrorTracks = hasSources ? apiErrorTracks : false;

  const onTrackPress = (track: DiscoveryResult): void => {
    setDetailHandoff({
      ...track,
      image_url: track.image_url ?? result.image_url,
    });
    router.push(detailRoute as '/discover/detail');
  };

  const onAlbumPress = (album: DiscoveryResult): void => {
    setDetailHandoff({ ...album, subtitle: album.subtitle ?? result.title });
    router.push(detailRoute as '/discover/detail');
  };

  return {
    hasSources,
    topTracks,
    isLoadingTracks,
    isErrorTracks,
    refetchTracks,
    libraryAlbums,
    apiAlbums,
    isLoadingAlbums,
    isErrorAlbums,
    refetchAlbums,
    exploreExpanded,
    setExploreExpanded,
    discoveryLoading: discoverySearch.isLoading,
    discoveryError: discoverySearch.isError,
    onTrackPress,
    onAlbumPress,
  };
}
