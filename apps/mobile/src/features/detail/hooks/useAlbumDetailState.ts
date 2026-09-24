import { useRef, useState, type Dispatch, type SetStateAction } from 'react';
import { useRouter } from 'expo-router';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { trackIdentityKey } from '@shared/acquisition/trackStatusStore';
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
import { runBounded, SAVE_ALL_CONCURRENCY, type BatchOutcome } from '../save-all';
import { trackExtras } from '../extras-accessors';
import { normalizeForCompare } from '../text-compare';
import { ownedFromExtras, type OwnedTrack } from './useOwnedTrack';

function _enrichAlbumTrack(track: DiscoveryResult, album: DiscoveryResult): DiscoveryResult {
  const ownExtras = trackExtras(track.extras);
  return {
    ...track,
    image_url: track.image_url ?? album.image_url,
    extras: {
      ...track.extras,
      album: ownExtras.album ?? album.title,
      album_artist: ownExtras.albumArtist ?? album.subtitle,
    },
  };
}

function _isTrackOwned(title: string, ownedTitles: Set<string>): boolean {
  return ownedTitles.has(normalizeForCompare(title));
}

function _unownedTracks(tracks: readonly DiscoveryResult[]): DiscoveryResult[] {
  return tracks.filter((t) => ownedFromExtras(trackExtras(t.extras)) === null);
}

// The same (title, artist) identity the save itself is keyed by, so a row, the batch
// that claims it, and the save's idempotency key all name the track the same way.
function _trackIdentity(track: DiscoveryResult): string | null {
  return trackIdentityKey(track.title, track.subtitle ?? '');
}

function _identitiesOf(tracks: readonly DiscoveryResult[]): Set<string> {
  const identities = new Set<string>();
  for (const track of tracks) {
    const identity = _trackIdentity(track);
    if (identity !== null) identities.add(identity);
  }
  return identities;
}

// A track with no usable identity cannot be remembered, so it is never skipped: the
// stable idempotency key, not this filter, is what keeps its re-save from duplicating.
function _notYetSaved(
  tracks: readonly DiscoveryResult[],
  saved: ReadonlySet<string>,
): DiscoveryResult[] {
  return tracks.filter((track) => {
    const identity = _trackIdentity(track);
    return identity === null || !saved.has(identity);
  });
}

const NO_CLAIMS: ReadonlySet<string> = new Set<string>();

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
  savingAll: boolean;
  // True while a "Save all" run owns this track's write — dispatched or still queued.
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
  const [saveAllClaims, setSaveAllClaims] = useState<ReadonlySet<string>>(NO_CLAIMS);
  // Identities a "Save all" run on this screen has already written. Held in a ref
  // because only the next tap reads it, never a render.
  const savedBySaveAll = useRef(new Set<string>());

  const discovery = useAlbumDiscovery({
    albumTitle: result.title,
    artist: result.subtitle,
    enabled: !hasSources && result.title !== '',
  });

  const ownedTitles = new Set(localTracks.map((t) => normalizeForCompare(t.title)));
  const moreTracks = discovery.tracks.filter((t) => !_isTrackOwned(t.title, ownedTitles));

  const { tracks, isLoading, failure } = hasSources
    ? { tracks: apiTracks, isLoading: apiLoading, failure: apiFailure }
    : {
        tracks: localAsDiscovery,
        isLoading: localTracks.length > 0 && discovery.isLoading,
        // The tracklist is the library's own, which cannot fail.
        failure: null,
      };

  const onTrackPress = (track: DiscoveryResult): void => {
    openDetail(router, detailRoute, _enrichAlbumTrack(track, result));
  };

  const recordSaveAllOutcome = (outcome: BatchOutcome<DiscoveryResult>): void => {
    for (const identity of _identitiesOf(outcome.succeeded)) {
      savedBySaveAll.current.add(identity);
    }
    if (outcome.failed.length === 0) return;
    // Each save logs its own reason; this line adds the one thing none of them carry —
    // how much of the batch got through — so a partial failure is visible as partial.
    console.warn('[detail] save all finished with failures', {
      album: result.title,
      saved: outcome.succeeded.length,
      failed: outcome.failed.length,
    });
  };

  const releaseSaveAll = (): void => {
    savingAllRef.current = false;
    setSavingAll(false);
    setSaveAllClaims(NO_CLAIMS);
  };

  const startSaveAll = (pending: readonly DiscoveryResult[]): void => {
    savingAllRef.current = true;
    setSavingAll(true);
    setSaveAllClaims(_identitiesOf(pending));
    const saveOne = (track: DiscoveryResult): Promise<unknown> =>
      save.mutateAsync(toCreateTrackRequest(_enrichAlbumTrack(track, result)));
    void runBounded(pending, SAVE_ALL_CONCURRENCY, saveOne)
      .then(recordSaveAllOutcome)
      .finally(releaseSaveAll);
  };

  const onSaveAll = (): void => {
    if (savingAllRef.current) return;
    const unowned = _unownedTracks(hasSources ? tracks : [...tracks, ...moreTracks]);
    const pending = _notYetSaved(unowned, savedBySaveAll.current);
    if (pending.length === 0) return;
    startSaveAll(pending);
  };

  const isSavingInBatch = (track: DiscoveryResult): boolean => {
    const identity = _trackIdentity(track);
    return identity !== null && saveAllClaims.has(identity);
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
    // Either discovery step failing — the search for this album, or the listing
    // of its tracks — leaves "More from this album" with nothing to show, and
    // one retry re-runs both. A search that ran and found nothing is not a
    // failure and stays an absent section.
    discoveryError: discovery.isError,
    discoveryFailure: discovery.failure,
    discoveryRefetch: () => {
      void discovery.refetch();
    },
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
