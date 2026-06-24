/**
 * useAlbumDetailState — the data + behaviour behind AlbumDetailBody.
 *
 * Owns the source resolution, the api/library track fetch, the "More from
 * this album" discovery expansion, and the save handlers, so the component
 * is left as a presentational shell. Lifted out of AlbumDetailBody to drop
 * its cognitive complexity and make the merge/save logic testable in isolation.
 */
import { useState, type Dispatch, type SetStateAction } from 'react';
import { useRouter } from 'expo-router';
import { useQueryClient } from '@tanstack/react-query';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { setDetailHandoff } from '@shared/lib/detail-handoff';

import { useAlbumDiscovery } from './useAlbumDiscovery';
import { useAlbumTracks } from './useAlbumTracks';
import { useLibraryTracksForAlbum, libraryTrackToDiscoveryResult } from './useLibraryTracks';
import { useSaveTrack } from './useSaveTrack';
import { toCreateTrackRequest } from '../save-cache';
import { _isTrackInLibraryCache } from '../ui/helpers';

function _enrichAlbumTrack(track: DiscoveryResult, album: DiscoveryResult): DiscoveryResult {
  return {
    ...track,
    image_url: track.image_url ?? album.image_url,
    extras: {
      ...track.extras,
      album: track.extras['album'] ?? album.title,
      album_artist: track.extras['album_artist'] ?? album.subtitle,
    },
  };
}

function _isTrackOwned(title: string, ownedTitles: Set<string>): boolean {
  return ownedTitles.has(title.toLowerCase().trim());
}

export type AlbumDetailState = {
  tracks: DiscoveryResult[];
  isLoading: boolean;
  isError: boolean;
  refetch: () => void;
  hasSources: boolean;
  moreExpanded: boolean;
  setMoreExpanded: Dispatch<SetStateAction<boolean>>;
  moreTracks: DiscoveryResult[];
  discoveryLoading: boolean;
  discoveryError: boolean;
  discoveryRefetch: () => void;
  saveAllTapped: boolean;
  savePending: boolean;
  onTrackPress: (track: DiscoveryResult) => void;
  onQuickSave: (track: DiscoveryResult) => void;
  onSaveAll: () => void;
  isSaved: (title: string, subtitle: string | null) => boolean;
};

export function useAlbumDetailState(
  result: DiscoveryResult,
  detailRoute: string,
  isFromLibrary?: boolean,
): AlbumDetailState {
  const router = useRouter();
  const queryClient = useQueryClient();
  const save = useSaveTrack();

  const source = !isFromLibrary ? result.sources[0] : undefined;
  const deezerSource = !isFromLibrary
    ? result.sources.find((s) => s.provider === 'deezer')
    : undefined;
  const effectiveSource = deezerSource ?? source;
  const hasSources = effectiveSource !== undefined;

  const { tracks: apiTracks, isLoading: apiLoading, isError: apiError, refetch } = useAlbumTracks({
    provider: effectiveSource?.provider ?? 'deezer',
    externalId: effectiveSource?.external_id ?? '_',
    albumTitle: result.title,
    albumArtist: result.subtitle ?? undefined,
    allSources: result.sources,
    enabled: hasSources || result.title !== '',
  });

  const localTracks = useLibraryTracksForAlbum(result.title, result.subtitle);
  const localAsDiscovery = localTracks.map(libraryTrackToDiscoveryResult);

  const [moreExpanded, setMoreExpanded] = useState(false);
  const [saveAllTapped, setSaveAllTapped] = useState(false);

  const discovery = useAlbumDiscovery({
    albumTitle: result.title,
    artist: result.subtitle,
    enabled: !hasSources && moreExpanded,
  });

  const ownedTitles = new Set(localTracks.map((t) => t.title.toLowerCase().trim()));
  const moreTracks = discovery.tracks.filter((t) => !_isTrackOwned(t.title, ownedTitles));

  const tracks = hasSources ? apiTracks : localAsDiscovery;
  const isLoading = hasSources ? apiLoading : false;
  const isError = hasSources ? apiError : false;

  const onTrackPress = (track: DiscoveryResult): void => {
    setDetailHandoff(_enrichAlbumTrack(track, result));
    router.push(detailRoute as '/discover/detail');
  };

  const onQuickSave = (track: DiscoveryResult): void => {
    save.mutate(toCreateTrackRequest(_enrichAlbumTrack(track, result)));
  };

  const onSaveAll = (): void => {
    setSaveAllTapped(true);
    const allTracks = hasSources ? tracks : [...tracks, ...moreTracks];
    for (const track of allTracks) {
      const enriched = _enrichAlbumTrack(track, result);
      if (!_isTrackInLibraryCache(queryClient, enriched.title, enriched.subtitle)) {
        save.mutate(toCreateTrackRequest(enriched));
      }
    }
  };

  const isSaved = (title: string, subtitle: string | null): boolean =>
    _isTrackInLibraryCache(queryClient, title, subtitle);

  return {
    tracks,
    isLoading,
    isError,
    refetch,
    hasSources,
    moreExpanded,
    setMoreExpanded,
    moreTracks,
    discoveryLoading: discovery.isLoading,
    discoveryError: discovery.isError,
    discoveryRefetch: discovery.refetch,
    saveAllTapped,
    savePending: save.isPending,
    onTrackPress,
    onQuickSave,
    onSaveAll,
    isSaved,
  };
}
