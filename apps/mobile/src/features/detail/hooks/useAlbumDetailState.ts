import { useRef, useState, type Dispatch, type SetStateAction } from 'react';
import { useRouter } from 'expo-router';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { trackToDiscoveryResult } from '@shared/lib/track-to-discovery';

import { type OwnedSplit } from '../owned-playback';
import { type ContentFailure } from '../content-status';

import { openDetail, type DetailRoute } from '../navigation';
import { useAlbumDiscovery } from './useAlbumDiscovery';
import { useAlbumTracks } from './useAlbumTracks';
import { useLibraryTracksForAlbum } from './useLibraryTracks';
import { useSaveTrack } from './useSaveTrack';
import { useOwnedPlayback } from './useOwnedPlayback';
import { toCreateTrackRequest } from '../save-cache';
import { runBounded, SAVE_ALL_CONCURRENCY } from '../save-all';
import { trackExtras } from '../extras-accessors';
import { normalizeForCompare } from '../text-compare';
import { ownedFromExtras, type OwnedTrack } from './useOwnedTrack';

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
  return ownedTitles.has(normalizeForCompare(title));
}

function _unownedTracks(tracks: readonly DiscoveryResult[]): DiscoveryResult[] {
  return tracks.filter((t) => ownedFromExtras(trackExtras(t.extras)) === null);
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
  discoveryLoading: boolean;
  discoveryError: boolean;
  discoveryFailure: ContentFailure | null;
  discoveryRefetch: () => void;
  savingAll: boolean;
  onTrackPress: (track: DiscoveryResult) => void;
  onQuickSave: (track: DiscoveryResult) => void;
  onSaveAll: () => void;
  ownedFor: (track: DiscoveryResult) => OwnedTrack | null;
  owned: OwnedSplit;
  playButton: { label: string; disabled: boolean };
  onPlayOwned: () => void;
};

export function useAlbumDetailState(
  result: DiscoveryResult,
  detailRoute: DetailRoute,
): AlbumDetailState {
  const router = useRouter();
  const save = useSaveTrack();

  const effectiveSource = result.sources.find((s) => s.provider === 'deezer') ?? result.sources[0];
  const hasSources = effectiveSource !== undefined;

  const {
    tracks: apiTracks,
    isLoading: apiLoading,
    failure: apiFailure,
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
  const [savingAll, setSavingAll] = useState(false);
  const savingAllRef = useRef(false);

  const discovery = useAlbumDiscovery({
    albumTitle: result.title,
    artist: result.subtitle,
    enabled: !hasSources && result.title !== '',
  });

  const ownedTitles = new Set(localTracks.map((t) => normalizeForCompare(t.title)));
  const moreTracks = discovery.tracks.filter((t) => !_isTrackOwned(t.title, ownedTitles));

  const tracks = hasSources ? apiTracks : localAsDiscovery;

  const isLoading = hasSources ? apiLoading : localTracks.length > 0 && discovery.isLoading;
  // Without sources the tracklist is the library's own, which cannot fail.
  const failure = hasSources ? apiFailure : null;

  const onTrackPress = (track: DiscoveryResult): void => {
    openDetail(router, detailRoute, _enrichAlbumTrack(track, result));
  };

  const onSaveAll = (): void => {
    if (savingAllRef.current) return;
    const unowned = _unownedTracks(hasSources ? tracks : [...tracks, ...moreTracks]);
    if (unowned.length === 0) return;
    savingAllRef.current = true;
    setSavingAll(true);
    const saveOne = (track: DiscoveryResult): Promise<unknown> =>
      save.mutateAsync(toCreateTrackRequest(_enrichAlbumTrack(track, result)));
    void runBounded(unowned, SAVE_ALL_CONCURRENCY, saveOne).finally(() => {
      savingAllRef.current = false;
      setSavingAll(false);
    });
  };

  const { owned, playButton, onPlayOwned, ownedFor, onQuickSave } = useOwnedPlayback(
    tracks,
    {
      title: result.subtitle,
      image: result.image_url,
      enrich: (track) => _enrichAlbumTrack(track, result),
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
    discoveryLoading: discovery.isLoading,
    // Only the tracks-for-album step failing means "found the album but couldn't
    // list its tracks" — the case the "More from this album" section can retry.
    // A failed search means the album was never found, so there is nothing to
    // show there and its absence is correct rather than an error.
    discoveryError: discovery.isTracksError,
    discoveryFailure: discovery.tracksFailure,
    discoveryRefetch: () => {
      void discovery.refetch();
    },
    savingAll,
    onTrackPress,
    onQuickSave,
    onSaveAll,
    ownedFor,
    owned,
    playButton,
    onPlayOwned,
  };
}
