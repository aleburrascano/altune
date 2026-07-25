import { useState, type Dispatch, type SetStateAction } from 'react';
import { useRouter } from 'expo-router';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { trackToDiscoveryResult } from '@shared/lib/track-to-discovery';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';

import { trackExtras } from '../extras-accessors';
import { openDetail, type DetailRoute } from '../navigation';
import { ownedFromExtras } from './useOwnedTrack';
import { saveControlState, type SaveControlState } from '../save-control-state';
import { playButtonState, splitOwned, toPlaybackQueue, type OwnedSplit } from '../owned-playback';
import { toCreateTrackRequest } from '../save-cache';
import { useArtistContent } from './useArtistContent';
import { useArtistDiscovery } from './useArtistDiscovery';
import { useLibraryAlbumsForArtist } from './useLibraryAlbumsForArtist';
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
  saveStateFor: (track: DiscoveryResult) => SaveControlState;
  owned: OwnedSplit;
  playButton: { label: string; disabled: boolean };
  onPlayOwned: () => void;
};

export function useArtistDetailState(
  result: DiscoveryResult,
  detailRoute: DetailRoute,
  isFromLibrary?: boolean,
): ArtistDetailState {
  const router = useRouter();
  const save = useSaveTrack();
  const queue = useQueuePlayback();
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

  const libraryTracksAsDiscovery = localTracks.map(trackToDiscoveryResult);
  const libraryAlbums = useLibraryAlbumsForArtist(result.title, !hasSources);

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

  const saveStateFor = (track: DiscoveryResult): SaveControlState =>
    saveControlState(ownedFromExtras(trackExtras(track.extras)));

  const owned = splitOwned(topTracks);

  const onPlayOwned = (): void => {
    const playable = toPlaybackQueue(owned.playable, result.title, result.image_url);
    if (playable.length === 0) return;
    queue.playFromList(playable, 0, { kind: 'library' });
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
    onQuickSave,
    saveStateFor,
    owned,
    playButton: playButtonState(owned),
    onPlayOwned,
  };
}
