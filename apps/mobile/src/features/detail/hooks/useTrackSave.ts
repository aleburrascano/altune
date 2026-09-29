import { useRef } from 'react';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import {
  ownedRetryTrackId,
  saveControlInteractive,
  saveControlState,
  type SaveState,
} from '../save-control-state';
import { toCreateTrackRequest } from '../save-cache';

import type { OwnedTrack } from './useOwnedTrack';
import { useRetryTrack, type RetryTrack } from './useRetryTrack';
import { useSaveTrack, type SaveFailure, type SaveTrack } from './useSaveTrack';

export type TrackSave = {
  state: SaveState;
  failure: SaveFailure | null;
  onSave: () => void;
};

function failureState(failure: SaveFailure): Exclude<SaveState, 'disabled'> {
  return failure.isRetryable ? 'failed' : 'rejected';
}

function rememberedFailure(owned: OwnedTrack | null): SaveFailure | null {
  if (owned?.acquisitionStatus !== 'failed' || owned.failureMessage === null) {
    return null;
  }
  return { message: owned.failureMessage, isRetryable: true };
}

function deriveState(
  canSave: boolean,
  save: { failure: SaveFailure | null; isPending: boolean },
  owned: OwnedTrack | null,
): SaveState {
  if (!canSave) return 'disabled';
  if (save.failure !== null) return failureState(save.failure);
  if (save.isPending) return 'saving';
  return saveControlState(owned);
}

type SaveDeps = {
  owned: OwnedTrack | null;
  track: DiscoveryResult;
  save: SaveTrack;
  retry: RetryTrack;
};

type Release = { onSettled: () => void };
type Guarded = (action: (release: Release) => void) => void;

function useLatch(): Guarded {
  const held = useRef(false);
  return (action) => {
    if (held.current) return;
    held.current = true;
    action({ onSettled: () => void (held.current = false) });
  };
}

function dispatchSave(deps: SaveDeps, release: Release): void {
  const retryId = ownedRetryTrackId(deps.owned);
  if (retryId !== null) deps.retry.mutate(retryId, release);
  else deps.save.mutate(toCreateTrackRequest(deps.track), release);
}

function buildOnSave(state: SaveState, deps: SaveDeps, guarded: Guarded): () => void {
  if (!saveControlInteractive(state)) return () => undefined;
  return () => guarded((release) => dispatchSave(deps, release));
}

export function useTrackSave(track: DiscoveryResult, owned: OwnedTrack | null): TrackSave {
  const guarded = useLatch();
  const save = useSaveTrack();
  const retry = useRetryTrack('detail');
  const canSave = (track.subtitle ?? '').length > 0;
  const state = deriveState(canSave, save, owned);
  const failure = save.failure ?? (state === 'failed' ? rememberedFailure(owned) : null);
  const onSave = buildOnSave(state, { owned, track, save, retry }, guarded);
  return { state, failure, onSave };
}
