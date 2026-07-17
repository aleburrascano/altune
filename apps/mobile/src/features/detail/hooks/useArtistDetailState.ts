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
import { useQueryClient } from '@tanstack/react-query';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { deriveAlbums } from '@shared/lib/derive-library-groups';
import { trackToDiscoveryResult } from '@shared/lib/track-to-discovery';

import { trackExtras } from '../extras-accessors';
import { openDetail, type DetailRoute } from '../navigation';
import { findTrackInLibraryCache } from '../helpers/find-track-in-library-cache';
import { saveControlState, type SaveControlState } from '../save-control-state';
import { toCreateTrackRequest } from '../save-cache';
import { useArtistContent } from './useArtistContent';
import { useArtistDiscovery } from './useArtistDiscovery';
import { useLibraryTracksForArtist } from './useLibraryTracks';
import { useSaveTrack } from './useSaveTrack';

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
  onQuickSave: (track: DiscoveryResult) => void;
  saveStateFor: (title: string, subtitle: string | null) => SaveControlState;
};

export function useArtistDetailState(
  result: DiscoveryResult,
  detailRoute: DetailRoute,
  isFromLibrary?: boolean,
): ArtistDetailState {
  const router = useRouter();
  const queryClient = useQueryClient();
  const save = useSaveTrack();
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

  // MBID for identity-safe Last.fm top-tracks — from the tapped artist when it has
  // its own sources, else from the name-resolved discovery result.
  const effectiveMbid =
    (hasSources ? trackExtras(result.extras).mbid : discoverySearch.mbid) ?? undefined;

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
    ...(effectiveMbid ? { mbid: effectiveMbid } : {}),
    enabled: shouldFetchContent,
  });

  const libraryTracksAsDiscovery = localTracks.map(trackToDiscoveryResult);
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
    openDetail(router, detailRoute, {
      ...track,
      image_url: track.image_url ?? result.image_url,
    });
  };

  const onAlbumPress = (album: DiscoveryResult): void => {
    openDetail(router, detailRoute, { ...album, subtitle: album.subtitle ?? result.title });
  };

  const onQuickSave = (track: DiscoveryResult): void => {
    save.mutate(
      toCreateTrackRequest({
        ...track,
        subtitle: track.subtitle ?? result.title,
        image_url: track.image_url ?? result.image_url,
      }),
    );
  };

  const saveStateFor = (title: string, subtitle: string | null): SaveControlState =>
    saveControlState(findTrackInLibraryCache(queryClient, title, subtitle));

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
    onQuickSave,
    saveStateFor,
  };
}
