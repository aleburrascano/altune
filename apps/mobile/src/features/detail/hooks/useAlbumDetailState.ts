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
import { trackToDiscoveryResult } from '@shared/lib/track-to-discovery';

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

/**
 * Give each owned track its real album position by matching it against the
 * authoritative provider tracklist (whose order IS the album order), then sort
 * by position. A track that already carries a stored position keeps it; tracks
 * with no match sink to the end. No-op until the tracklist has loaded.
 */
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
};

export function useAlbumDetailState(
  result: DiscoveryResult,
  detailRoute: DetailRoute,
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
  const localAsDiscovery = localTracks.map(trackToDiscoveryResult);

  const [moreExpanded, setMoreExpanded] = useState(false);
  const [saveAllTapped, setSaveAllTapped] = useState(false);

  // Fetch eagerly (cached 30min) rather than on-expand, so the "More from this
  // album" affordance can be hidden entirely when there's genuinely nothing to
  // add — a single, or an album you already own in full. Without this the
  // expander showed unconditionally and dead-ended on "you have all tracks".
  const discovery = useAlbumDiscovery({
    albumTitle: result.title,
    artist: result.subtitle,
    enabled: !hasSources && result.title !== '',
  });

  const ownedTitles = new Set(localTracks.map((t) => t.title.toLowerCase().trim()));

  // The provider tracklist's ORDER is the album order — stamp each track with its
  // 1-based position so BOTH the owned rows and the "More from this album" rows
  // show the real album number. (The "more" rows previously counted on from the
  // owned-track count, so their numbers were wrong.)
  const albumOrder = discovery.tracks.map((t, i) =>
    trackExtras(t.extras).trackPosition != null
      ? t
      : { ...t, extras: { ...t.extras, track_position: i + 1 } },
  );
  const moreTracks = albumOrder.filter((t) => !_isTrackOwned(t.title, ownedTitles));

  // Owned tracks saved before track_number was sent have no position, so the
  // list falls back to counting 1..N. Recover the real album order by matching
  // each owned track against the album order and sorting by it. Display-only +
  // re-derived per view, so it never goes stale; a stored position always wins.
  const ownedTracks = !hasSources
    ? _withAlbumPositions(localAsDiscovery, albumOrder)
    : localAsDiscovery;

  const tracks = hasSources ? apiTracks : ownedTracks;

  // Persist the derived positions back to the DB (fill-only) so tracks saved
  // before track_number was captured self-heal as their album is opened.
  usePersistTrackNumbers(localTracks, ownedTracks);

  // Hold a library album's list until the album order has loaded, so positions
  // are correct on first paint instead of visibly re-sorting after the screen
  // appears. Only when there are owned tracks to reorder (an empty album shows
  // its empty state immediately); cached after first open, and if the lookup
  // fails the list still renders in index order.
  const isLoading = hasSources
    ? apiLoading
    : localTracks.length > 0 && discovery.isLoading;
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
  };
}
