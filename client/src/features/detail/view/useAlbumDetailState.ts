import { useState, type Dispatch, type SetStateAction } from 'react';
import type { DiscoveryResult } from '@shared/catalog';
import { trackToDiscoveryResult } from '@shared/catalog';
import type { OwnedSplit, OwnedTrack } from '@features/detail/core/ownedTrack';
import { type ContentFailure } from '@features/detail/core/contentStatus';

import { type DetailRoute } from '@features/detail/core/detailNavigation';
import { useOpenDetail } from '@features/detail/view/useDetailNavigation';
import { useAlbumDiscovery } from '@features/detail/view/useEntityDiscovery';
import { useAlbumTracks } from '@features/detail/view/useAlbumTracks';
import { useLibraryTracksForAlbum } from '@features/detail/view/useLibraryMatches';
import { useSaveTrack } from '@features/detail/view/useSaveTrack';
import {
  enrichAlbumTrack,
  useAlbumSaveAll,
  type LastBatch,
} from '@features/detail/view/useAlbumSaveAll';
import { useOwnedPlayback } from '@features/detail/view/useOwnedPlayback';
import { trackExtras } from '@features/detail/core/trackExtras';
import { normalizeForCompare } from '@features/detail/core/normalizeForCompare';

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

type AlbumSource = DiscoveryResult['sources'][number];

function effectiveSourceOf(album: DiscoveryResult): AlbumSource | undefined {
  return album.sources.find((s) => s.provider === 'deezer') ?? album.sources[0];
}

function sourceLookup(source: AlbumSource | undefined) {
  return {
    provider: source?.provider ?? 'deezer',
    externalId: source?.external_id ?? '_',
  };
}

function albumTracksArgs(album: DiscoveryResult, source: AlbumSource | undefined) {
  return {
    ...sourceLookup(source),
    albumTitle: album.title,
    albumArtist: album.subtitle ?? undefined,
    allSources: album.sources,
    enabled: source !== undefined || album.title !== '',
  };
}

type ShownTracks = Pick<AlbumDetailState, 'tracks' | 'isLoading' | 'failure'>;

type LocalAlbumTracks = { tracks: DiscoveryResult[]; hasAny: boolean; discoveryLoading: boolean };

function shownTracks(hasSources: boolean, api: ShownTracks, local: LocalAlbumTracks): ShownTracks {
  if (hasSources) return api;
  const isLoading = local.hasAny && local.discoveryLoading;
  return { tracks: local.tracks, isLoading, failure: null };
}

export function useAlbumDetailState(
  album: DiscoveryResult,
  detailRoute: DetailRoute,
): AlbumDetailState {
  const openDetail = useOpenDetail(detailRoute);
  const save = useSaveTrack();

  const effectiveSource = effectiveSourceOf(album);
  const hasSources = effectiveSource !== undefined;

  const {
    tracks: apiTracks,
    isLoading: apiLoading,
    failure: apiFailure,
    refetch,
  } = useAlbumTracks(albumTracksArgs(album, effectiveSource));

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

  const { tracks, isLoading, failure } = shownTracks(
    hasSources,
    { tracks: apiTracks, isLoading: apiLoading, failure: apiFailure },
    {
      tracks: localAsDiscovery,
      hasAny: localTracks.length > 0,
      discoveryLoading: discovery.isLoading,
    },
  );

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
