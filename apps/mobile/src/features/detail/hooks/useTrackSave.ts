import type { DiscoveryResult } from '@shared/api-client/discovery';

import { ownedRetryTrackId, saveControlInteractive, saveControlState, type SaveState } from '../save-control-state';
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

type SaveDeps = { owned: OwnedTrack | null; track: DiscoveryResult; save: SaveTrack; retry: RetryTrack };

function saveOrRetry(deps: SaveDeps): () => void {
  const retryId = ownedRetryTrackId(deps.owned);
  if (retryId !== null) return () => deps.retry.mutate(retryId);
  return () => deps.save.mutate(toCreateTrackRequest(deps.track));
}

function buildOnSave(state: SaveState, deps: SaveDeps): () => void {
  if (!saveControlInteractive(state)) return () => undefined;
  return saveOrRetry(deps);
}

export function useTrackSave(track: DiscoveryResult, owned: OwnedTrack | null): TrackSave {
  const save = useSaveTrack();
  const retry = useRetryTrack('detail');
  const canSave = (track.subtitle ?? '').length > 0;
  const state = deriveState(canSave, save, owned);
  const failure = save.failure ?? (state === 'failed' ? rememberedFailure(owned) : null);
  const onSave = buildOnSave(state, { owned, track, save, retry });
  return { state, failure, onSave };
}
