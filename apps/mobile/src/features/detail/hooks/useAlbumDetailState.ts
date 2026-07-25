import { useState, type Dispatch, type SetStateAction } from 'react';
import { useRouter } from 'expo-router';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { trackToDiscoveryResult } from '@shared/lib/track-to-discovery';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';

import { playButtonState, splitOwned, toPlaybackQueue, type OwnedSplit } from '../owned-playback';

import { openDetail, type DetailRoute } from '../navigation';
import { useAlbumDiscovery } from './useAlbumDiscovery';
import { useAlbumTracks } from './useAlbumTracks';
import { useLibraryTracksForAlbum } from './useLibraryTracks';
import { useSaveTrack } from './useSaveTrack';
import { toCreateTrackRequest } from '../save-cache';
import { trackExtras } from '../extras-accessors';
import { ownedFromExtras } from './useOwnedTrack';
import { saveControlState, type SaveControlState } from '../save-control-state';

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

function byTrackPosition(a: DiscoveryResult, b: DiscoveryResult): number {
  const pa = trackExtras(a.extras).trackPosition ?? Number.MAX_SAFE_INTEGER;
  const pb = trackExtras(b.extras).trackPosition ?? Number.MAX_SAFE_INTEGER;
  return pa - pb;
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
  saveStateFor: (track: DiscoveryResult) => SaveControlState;
  owned: OwnedSplit;
  playButton: { label: string; disabled: boolean };
  onPlayOwned: () => void;
};

export function useAlbumDetailState(
  result: DiscoveryResult,
  detailRoute: DetailRoute,
  isFromLibrary?: boolean,
): AlbumDetailState {
  const router = useRouter();
  const save = useSaveTrack();
  const queue = useQueuePlayback();

  const source = !isFromLibrary ? result.sources[0] : undefined;
  const deezerSource = !isFromLibrary
    ? result.sources.find((s) => s.provider === 'deezer')
    : undefined;
  const effectiveSource = deezerSource ?? source;
  const hasSources = effectiveSource !== undefined;

  const {
    tracks: apiTracks,
    isLoading: apiLoading,
    isError: apiError,
    refetch,
  } = useAlbumTracks({
    provider: effectiveSource?.provider ?? 'deezer',
    externalId: effectiveSource?.external_id ?? '_',
    albumTitle: result.title,
    albumArtist: result.subtitle ?? undefined,
    allSources: result.sources,
    enabled: hasSources || result.title !== '',
  });

  const localTracks = useLibraryTracksForAlbum(result.title, result.subtitle);
  const localAsDiscovery = [...localTracks.map(trackToDiscoveryResult)].sort(byTrackPosition);

  const [moreExpanded, setMoreExpanded] = useState(false);
  const [saveAllTapped, setSaveAllTapped] = useState(false);

  const discovery = useAlbumDiscovery({
    albumTitle: result.title,
    artist: result.subtitle,
    enabled: !hasSources && result.title !== '',
  });

  const ownedTitles = new Set(localTracks.map((t) => t.title.toLowerCase().trim()));
  const moreTracks = discovery.tracks.filter((t) => !_isTrackOwned(t.title, ownedTitles));

  const tracks = hasSources ? apiTracks : localAsDiscovery;

  const isLoading = hasSources ? apiLoading : localTracks.length > 0 && discovery.isLoading;
  const isError = hasSources ? apiError : false;

  const onTrackPress = (track: DiscoveryResult): void => {
    openDetail(router, detailRoute, _enrichAlbumTrack(track, result));
  };

  const onQuickSave = (track: DiscoveryResult): void => {
    save.mutate(toCreateTrackRequest(_enrichAlbumTrack(track, result)));
  };

  const onSaveAll = (): void => {
    setSaveAllTapped(true);
    const allTracks = hasSources ? tracks : [...tracks, ...moreTracks];
    for (const track of allTracks) {
      if (ownedFromExtras(trackExtras(track.extras)) === null) {
        save.mutate(toCreateTrackRequest(_enrichAlbumTrack(track, result)));
      }
    }
  };

  const saveStateFor = (track: DiscoveryResult): SaveControlState =>
    saveControlState(ownedFromExtras(trackExtras(track.extras)));

  const owned = splitOwned(tracks);

  const onPlayOwned = (): void => {
    const playable = toPlaybackQueue(owned.playable, result.subtitle, result.image_url);
    if (playable.length === 0) return;
    queue.playFromList(playable, 0, { kind: 'library' });
  };

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
    saveStateFor,
    owned,
    playButton: playButtonState(owned),
    onPlayOwned,
  };
}
