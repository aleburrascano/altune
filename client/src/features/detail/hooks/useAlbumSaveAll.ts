import { useEffect, useRef, useState } from 'react';
import type { RefObject } from 'react';

import type { DiscoveryResult } from '@shared/apiClient/discovery';
import { currentSessionEpoch, isSameSession } from '@shared/session/signOutCleanup';
import { trackIdentityKey } from '@shared/acquisition/trackStatusStore';

import { runBounded, type BatchOutcome } from '@shared/runBounded';

import { toCreateTrackRequest, SAVE_ALL_CONCURRENCY } from '@features/detail/domain/saveTrack';
import { trackExtras } from '@features/detail/domain/trackExtras';
import { ownedFromExtras } from '@features/detail/domain/ownedTrack';
import type { useSaveTrack } from '@features/detail/hooks/useSaveTrack';

function _albumExtras(track: DiscoveryResult, album: DiscoveryResult): DiscoveryResult['extras'] {
  const ownExtras = trackExtras(track.extras);
  return {
    ...track.extras,
    album: ownExtras.album ?? album.title,
    album_artist: ownExtras.albumArtist ?? album.subtitle,
  };
}

export function enrichAlbumTrack(track: DiscoveryResult, album: DiscoveryResult): DiscoveryResult {
  return {
    ...track,
    image_url: track.image_url ?? album.image_url,
    extras: _albumExtras(track, album),
  };
}

function _unownedTracks(tracks: readonly DiscoveryResult[]): DiscoveryResult[] {
  return tracks.filter((t) => ownedFromExtras(trackExtras(t.extras)) === null);
}

function _ownedTracks(tracks: readonly DiscoveryResult[]): DiscoveryResult[] {
  return tracks.filter((t) => ownedFromExtras(trackExtras(t.extras)) !== null);
}

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

type AlbumSaveAllArgs = {
  album: DiscoveryResult;
  candidates: readonly DiscoveryResult[];
  libraryComplete: boolean;
  save: ReturnType<typeof useSaveTrack>;
};

export type LastBatch = { saved: number; failed: number };

type AlbumSaveAll = {
  lastBatch: LastBatch | null;
  savingAll: boolean;
  isSavingInBatch: (track: DiscoveryResult) => boolean;
  onSaveAll: () => void;
};

function _isClaimed(claims: ReadonlySet<string>, track: DiscoveryResult): boolean {
  const identity = _trackIdentity(track);
  return identity !== null && claims.has(identity);
}

function useBatchLock() {
  const [savingAll, setSavingAll] = useState(false);
  const activeRef = useRef(false);
  const set = (on: boolean): void => {
    activeRef.current = on;
    setSavingAll(on);
  };
  return { savingAll, activeRef, set };
}

function useBatchState() {
  const lock = useBatchLock();
  const [claims, setClaims] = useState<ReadonlySet<string>>(NO_CLAIMS);
  const setBatch = (pending: readonly DiscoveryResult[] | null): void => {
    lock.set(pending !== null);
    setClaims(pending === null ? NO_CLAIMS : _identitiesOf(pending));
  };
  const isSavingInBatch = (track: DiscoveryResult): boolean => _isClaimed(claims, track);
  return { savingAll: lock.savingAll, activeRef: lock.activeRef, isSavingInBatch, setBatch };
}

function _rememberUnowned(
  saved: Set<string>,
  succeeded: readonly DiscoveryResult[],
  owned: ReadonlySet<string>,
): void {
  for (const identity of _identitiesOf(succeeded)) {
    if (!owned.has(identity)) saved.add(identity);
  }
}

function useOwnedIdentities(
  candidates: readonly DiscoveryResult[],
): RefObject<ReadonlySet<string>> {
  const owned = useRef<ReadonlySet<string>>(NO_CLAIMS);
  useEffect(() => {
    owned.current = _identitiesOf(_ownedTracks(candidates));
  }, [candidates]);
  return owned;
}

function useOutcomeRecorder(owned: RefObject<ReadonlySet<string>>) {
  const [saved] = useState(() => new Set<string>());
  const [lastBatch, setLastBatch] = useState<LastBatch | null>(null);
  const record = (outcome: BatchOutcome<DiscoveryResult>): void => {
    _rememberUnowned(saved, outcome.succeeded, owned.current);
    setLastBatch({ saved: outcome.succeeded.length, failed: outcome.failed.length });
  };
  return { saved, lastBatch, record, clear: () => setLastBatch(null) };
}

function useForgetOnceOwned(saved: Set<string>, candidates: readonly DiscoveryResult[]): void {
  useEffect(() => {
    for (const identity of _identitiesOf(_ownedTracks(candidates))) saved.delete(identity);
  }, [saved, candidates]);
}

function useSaveAllState(candidates: readonly DiscoveryResult[]) {
  const batch = useBatchState();
  const outcomes = useOutcomeRecorder(useOwnedIdentities(candidates));
  useForgetOnceOwned(outcomes.saved, candidates);
  return { ...batch, ...outcomes };
}

function _runBatch(
  pending: readonly DiscoveryResult[],
  args: AlbumSaveAllArgs,
): Promise<BatchOutcome<DiscoveryResult>> {
  const saveOne = (track: DiscoveryResult): Promise<unknown> =>
    args.save.mutateAsync(toCreateTrackRequest(enrichAlbumTrack(track, args.album)));
  const epoch = currentSessionEpoch();
  return runBounded(pending, SAVE_ALL_CONCURRENCY, saveOne, () => isSameSession(epoch));
}

function _startBatch(
  pending: readonly DiscoveryResult[],
  args: AlbumSaveAllArgs,
  state: ReturnType<typeof useSaveAllState>,
): void {
  state.clear();
  state.setBatch(pending);
  void _runBatch(pending, args)
    .then(state.record)
    .finally(() => state.setBatch(null));
}

export function useAlbumSaveAll(args: AlbumSaveAllArgs): AlbumSaveAll {
  const state = useSaveAllState(args.candidates);
  const onSaveAll = (): void => {
    if (state.activeRef.current || !args.libraryComplete) return;
    const pending = _notYetSaved(_unownedTracks(args.candidates), state.saved);
    if (pending.length > 0) _startBatch(pending, args, state);
  };
  return {
    lastBatch: state.lastBatch,
    savingAll: state.savingAll,
    isSavingInBatch: state.isSavingInBatch,
    onSaveAll,
  };
}
