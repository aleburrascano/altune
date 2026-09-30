import { useRef, useState } from 'react';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { currentSessionEpoch, isSameSession } from '@shared/session/signOutCleanup';
import { trackIdentityKey } from '@shared/acquisition/trackStatusStore';

import { toCreateTrackRequest } from '../save-cache';
import { runBounded, SAVE_ALL_CONCURRENCY, type BatchOutcome } from '../save-all';
import { trackExtras } from '../extras-accessors';
import { ownedFromExtras } from './useOwnedTrack';
import type { useSaveTrack } from './useSaveTrack';

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

type AlbumSaveAll = {
  savingAll: boolean;
  isSavingInBatch: (track: DiscoveryResult) => boolean;
  onSaveAll: () => void;
};

function _isClaimed(claims: ReadonlySet<string>, track: DiscoveryResult): boolean {
  const identity = _trackIdentity(track);
  return identity !== null && claims.has(identity);
}

function _warnIfFailed(album: DiscoveryResult, outcome: BatchOutcome<DiscoveryResult>): void {
  if (outcome.failed.length === 0) return;
  console.warn('[detail] save all finished with failures', {
    album: album.title,
    saved: outcome.succeeded.length,
    failed: outcome.failed.length,
  });
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

function useOutcomeRecorder(album: DiscoveryResult) {
  const saved = useRef(new Set<string>());
  const record = (outcome: BatchOutcome<DiscoveryResult>): void => {
    for (const identity of _identitiesOf(outcome.succeeded)) saved.current.add(identity);
    _warnIfFailed(album, outcome);
  };
  return { saved, record };
}

function useSaveAllState(album: DiscoveryResult) {
  const batch = useBatchState();
  const outcomes = useOutcomeRecorder(album);
  return { ...batch, saved: outcomes.saved, record: outcomes.record };
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
  state.setBatch(pending);
  void _runBatch(pending, args)
    .then(state.record)
    .finally(() => state.setBatch(null));
}

export function useAlbumSaveAll(args: AlbumSaveAllArgs): AlbumSaveAll {
  const state = useSaveAllState(args.album);
  const onSaveAll = (): void => {
    if (state.activeRef.current || !args.libraryComplete) return;
    const pending = _notYetSaved(_unownedTracks(args.candidates), state.saved.current);
    if (pending.length > 0) _startBatch(pending, args, state);
  };
  return { savingAll: state.savingAll, isSavingInBatch: state.isSavingInBatch, onSaveAll };
}
