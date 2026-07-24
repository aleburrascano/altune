import { useState, type Dispatch, type SetStateAction } from 'react';
import { useRouter } from 'expo-router';
import { useQueryClient } from '@tanstack/react-query';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { trackToDiscoveryResult } from '@shared/lib/track-to-discovery';
import { buildPlayableQueue } from '@shared/playback/playFromList';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';

import { playButtonState, splitOwned, type OwnedSplit } from '../owned-playback';

import { openDetail, type DetailRoute } from '../navigation';
import { useAlbumDiscovery } from './useAlbumDiscovery';
import { useAlbumTracks } from './useAlbumTracks';
import { useLibraryTracksForAlbum } from './useLibraryTracks';
import { usePersistTrackNumbers } from './usePersistTrackNumbers';
import { useSaveTrack } from './useSaveTrack';
import { toCreateTrackRequest } from '../save-cache';
import { trackExtras } from '../extras-accessors';
import { findTrackInLibraryCache } from '../helpers/find-track-in-library-cache';
import { saveControlState, type SaveControlState } from '../save-control-state';
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

const _norm = (s: string): string => s.toLowerCase().trim();

export function _withAlbumPositions(
  owned: DiscoveryResult[],
  albumOrder: DiscoveryResult[],
): DiscoveryResult[] {
  if (albumOrder.length === 0) {
    return owned;
  }
  const positionByTitle = new Map<string, number>();
  albumOrder.forEach((t, i) => {
    positionByTitle.set(_norm(t.title), trackExtras(t.extras).trackPosition ?? i + 1);
  });
  const placed = owned.map((t) => {
    if (trackExtras(t.extras).trackPosition != null) {
      return t;
    }
    const pos = positionByTitle.get(_norm(t.title));
    return pos == null ? t : { ...t, extras: { ...t.extras, track_position: pos } };
  });
  return [...placed].sort(
    (a, b) =>
      (trackExtras(a.extras).trackPosition ?? Number.MAX_SAFE_INTEGER) -
      (trackExtras(b.extras).trackPosition ?? Number.MAX_SAFE_INTEGER),
  );
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
  saveStateFor: (title: string, subtitle: string | null) => SaveControlState;
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
  const queryClient = useQueryClient();
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
  const localAsDiscovery = localTracks.map(trackToDiscoveryResult);

  const [moreExpanded, setMoreExpanded] = useState(false);
  const [saveAllTapped, setSaveAllTapped] = useState(false);

  const discovery = useAlbumDiscovery({
    albumTitle: result.title,
    artist: result.subtitle,
    enabled: !hasSources && result.title !== '',
  });

  const ownedTitles = new Set(localTracks.map((t) => t.title.toLowerCase().trim()));

  const albumOrder = discovery.tracks.map((t, i) =>
    trackExtras(t.extras).trackPosition != null
      ? t
      : { ...t, extras: { ...t.extras, track_position: i + 1 } },
  );
  const moreTracks = albumOrder.filter((t) => !_isTrackOwned(t.title, ownedTitles));

  const ownedTracks = !hasSources
    ? _withAlbumPositions(localAsDiscovery, albumOrder)
    : localAsDiscovery;

  const tracks = hasSources ? apiTracks : ownedTracks;

  usePersistTrackNumbers(localTracks, ownedTracks);

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
      const enriched = _enrichAlbumTrack(track, result);
      if (!_isTrackInLibraryCache(queryClient, enriched.title, enriched.subtitle)) {
        save.mutate(toCreateTrackRequest(enriched));
      }
    }
  };

  const isSaved = (title: string, subtitle: string | null): boolean =>
    _isTrackInLibraryCache(queryClient, title, subtitle);

  const owned = splitOwned(tracks, (title, subtitle) =>
    findTrackInLibraryCache(queryClient, title, subtitle),
  );

  const onPlayOwned = (): void => {
    const first = owned.playable[0];
    if (first === undefined) return;
    const { playable, startIndex } = buildPlayableQueue(owned.playable, first.id);
    queue.playFromList(playable, startIndex, { kind: 'library' });
  };

  const saveStateFor = (title: string, subtitle: string | null): SaveControlState =>
    saveControlState(findTrackInLibraryCache(queryClient, title, subtitle));

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
    saveStateFor,
    owned,
    playButton: playButtonState(owned),
    onPlayOwned,
  };
}
