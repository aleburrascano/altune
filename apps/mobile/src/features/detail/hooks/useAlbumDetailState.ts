import { useState, type Dispatch, type SetStateAction } from 'react';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { trackToDiscoveryResult } from '@shared/lib/track-to-discovery';

import { type OwnedSplit } from '../owned-playback';
import { type ContentFailure } from '../content-status';

import { useOpenDetail, type DetailRoute } from '../navigation';
import { useAlbumDiscovery } from './useAlbumDiscovery';
import { useAlbumTracks } from './useAlbumTracks';
import { useLibraryTracksForAlbum } from './useLibraryTracks';
import { useSaveTrack } from './useSaveTrack';
import { enrichAlbumTrack, useAlbumSaveAll, type LastBatch } from './useAlbumSaveAll';
import { useOwnedPlayback } from './useOwnedPlayback';
import { trackExtras } from '../extras-accessors';
import { normalizeForCompare } from '../text-compare';
import type { OwnedTrack } from './useOwnedTrack';

function _isTrackOwned(title: string, ownedTitles: Set<string>): boolean {
  return ownedTitles.has(normalizeForCompare(title));
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
  failure: ContentFailure | null;
  refetch: () => void;
  hasSources: boolean;
  moreExpanded: boolean;
  setMoreExpanded: Dispatch<SetStateAction<boolean>>;
  moreTracks: DiscoveryResult[];
  discoveryError: boolean;
  discoveryFailure: ContentFailure | null;
  discoveryRefetch: () => void;
  lastBatch?: LastBatch | null;
  savingAll: boolean;
  isSavingInBatch: (track: DiscoveryResult) => boolean;
  onTrackPress: (track: DiscoveryResult) => void;
  onQuickSave: (track: DiscoveryResult) => void;
  onSaveAll: () => void;
  ownedFor: (track: DiscoveryResult) => OwnedTrack | null;
  owned: OwnedSplit;
  playButton: { label: string; disabled: boolean };
  onPlayOwned: () => void;
};

export function useAlbumDetailState(
  album: DiscoveryResult,
  detailRoute: DetailRoute,
): AlbumDetailState {
  const openDetail = useOpenDetail(detailRoute);
  const save = useSaveTrack();

  const effectiveSource = album.sources.find((s) => s.provider === 'deezer') ?? album.sources[0];
  const hasSources = effectiveSource !== undefined;

  const {
    tracks: apiTracks,
    isLoading: apiLoading,
    failure: apiFailure,
    refetch,
  } = useAlbumTracks({
    provider: effectiveSource?.provider ?? 'deezer',
    externalId: effectiveSource?.external_id ?? '_',
    albumTitle: album.title,
    albumArtist: album.subtitle ?? undefined,
    allSources: album.sources,
    enabled: hasSources || album.title !== '',
  });

  const localTracks = useLibraryTracksForAlbum(album.title, album.subtitle);
  const localAsDiscovery = [...localTracks.map(trackToDiscoveryResult)].sort(byTrackPosition);

  const [moreExpanded, setMoreExpanded] = useState(false);

  const discovery = useAlbumDiscovery({
    albumTitle: album.title,
    artist: album.subtitle,
    enabled: !hasSources && album.title !== '',
  });

  const ownedTitles = new Set(localTracks.map((t) => normalizeForCompare(t.title)));
  const moreTracks = discovery.tracks.filter((t) => !_isTrackOwned(t.title, ownedTitles));

  const { tracks, isLoading, failure } = hasSources
    ? { tracks: apiTracks, isLoading: apiLoading, failure: apiFailure }
    : {
        tracks: localAsDiscovery,
        isLoading: localTracks.length > 0 && discovery.isLoading,
        failure: null,
      };

  const onTrackPress = (track: DiscoveryResult): void => {
    openDetail(enrichAlbumTrack(track, album));
  };

  const { lastBatch, savingAll, isSavingInBatch, onSaveAll } = useAlbumSaveAll({
    album: album,
    candidates: hasSources ? tracks : [...tracks, ...moreTracks],
    libraryComplete: localTracks.complete !== false,
    save,
  });

  const { owned, playButton, onPlayOwned, ownedFor, onQuickSave } = useOwnedPlayback(
    tracks,
    {
      title: album.subtitle,
      image: album.image_url,
      enrich: (track) => enrichAlbumTrack(track, album),
      retryEntryPoint: 'album_row',
    },
    save,
  );

  return {
    tracks,
    isLoading,
    isError: failure !== null,
    failure,
    refetch,
    hasSources,
    moreExpanded,
    setMoreExpanded,
    moreTracks,
    discoveryError: discovery.isError,
    discoveryFailure: discovery.failure,
    discoveryRefetch: () => {
      void discovery.refetch();
    },
    lastBatch,
    savingAll,
    isSavingInBatch,
    onTrackPress,
    onQuickSave,
    onSaveAll,
    ownedFor,
    owned,
    playButton,
    onPlayOwned,
  };
}
