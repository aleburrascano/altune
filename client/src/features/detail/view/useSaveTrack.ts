import { useQueryClient, type QueryClient, type UseMutationResult } from '@tanstack/react-query';
import { useRef } from 'react';

import { correlationIdOf, describeError, isRetryable } from '@shared/failure';
import {
  type TrackId,
  type CreateTrackRequest,
  type TrackResponse,
  type DiscoveryResult,
  acquisitionOf,
  toFailed,
  toReady,
  toTrackStatus,
  type WireTrackStatus,
} from '@shared/catalog';
import {
  rememberDownloadMeta,
  adoptResponseStatus,
  linkTrackIdentity,
  failedTrackStatus,
  patchTrackStatus,
  removeTrackStatus,
  trackIdentityKey,
} from '@shared/acquisition';
import {
  invalidateLibraryDerived,
  removeTrackFromCaches,
  replaceTrackInCaches,
  upsertTrackInCaches,
} from '@shared/events';
import { useAppMutation } from '@shared/query';
import { guardedMutationOptions } from '@shared/session';
import { enqueueCritical } from '@shared/telemetry';
import {
  type SaveFailure,
  optimisticTrack,
  optimisticTrackId,
  saveIdempotencyKey,
  ownedRetryTrackId,
  saveControlInteractive,
  saveControlState,
  type SaveState,
  toCreateTrackRequest,
} from '@features/detail/core/saveTrack';
import { useDetailHandoff } from '@features/detail/view/useDetailHandoff';
import type { DetailPort } from '@features/detail/ports/detailPort';
import { useDetailPort } from '@features/detail/view/DetailPortProvider';
import type { OwnedTrack } from '@features/detail/core/ownedTrack';
import { useRetryTrack, type RetryTrack } from '@features/detail/view/useRetryTrack';

type SaveContext = { optimisticId: TrackId; identity: string | null };

type SaveMutation = UseMutationResult<TrackResponse, Error, CreateTrackRequest, SaveContext>;

export type SaveTrack = {
  mutate: SaveMutation['mutate'];
  mutateAsync: SaveMutation['mutateAsync'];
  isPending: boolean;
  failure: SaveFailure | null;
};

const SAVE_ACTION = 'save this track';
const REMEMBERED_FAILURE_TITLE = "Couldn't save this track";

type SaveBody = CreateTrackRequest | undefined;

function saveFailure(error: Error | null, body: SaveBody): SaveFailure | null {
  if (error === null) return null;
  return {
    copy: describeError(error, SAVE_ACTION),
    isRetryable: isRetryable(error),
    ...(body === undefined ? {} : { trackId: optimisticTrackId(body) }),
  };
}

type Handoff = ReturnType<typeof useDetailHandoff>;

function sendSave(port: DetailPort, body: CreateTrackRequest): Promise<TrackResponse> {
  return port.createTrack(body, saveIdempotencyKey(body)).then((saved) => {
    rememberDownloadMeta(saved.id, {
      title: saved.title,
      artist: saved.artist,
      artworkUrl: saved.artwork_url,
    });
    return saved;
  });
}

function markOptimistic(placeholder: TrackResponse, identity: string | null): void {
  patchTrackStatus(
    placeholder.id,
    { acquisitionStatus: 'pending', failureMessage: null },
    'optimistic',
  );
  linkTrackIdentity(identity, placeholder.id);
}

function placeOptimistic(queryClient: QueryClient, body: CreateTrackRequest): SaveContext {
  const placeholder = optimisticTrack(body, new Date().toISOString());
  upsertTrackInCaches(queryClient, placeholder);
  const identity = trackIdentityKey(body.title, body.artist);
  markOptimistic(placeholder, identity);
  return { optimisticId: placeholder.id, identity };
}

function libraryAddPayload(body: CreateTrackRequest, handoff: Handoff) {
  const signature = handoff?.result.result_signature;
  return {
    title: body.title,
    artist: body.artist,
    album: body.album,
    year: body.year,
    ...(signature != null ? { result_signature: signature } : {}),
  };
}

function recordLibraryAdd(body: CreateTrackRequest, handoff: Handoff): void {
  void enqueueCritical({
    type: 'library_add',
    search_id: handoff?.searchId ?? undefined,
    payload: libraryAddPayload(body, handoff),
  });
}

function withStatus(saved: TrackResponse, status: WireTrackStatus): TrackResponse {
  if (status.acquisitionStatus === 'pending') return saved;
  if (status.acquisitionStatus === 'ready') return { ...saved, ...toReady() };
  return { ...saved, ...toFailed(null, status.failureMessage) };
}

function adoptSaved(queryClient: QueryClient, saved: TrackResponse, context: SaveContext): void {
  const status = adoptResponseStatus(saved.id, toTrackStatus(acquisitionOf(saved)));
  replaceTrackInCaches(queryClient, context.optimisticId, withStatus(saved, status));
  removeTrackStatus(context.optimisticId);
  linkTrackIdentity(context.identity, saved.id);
  invalidateLibraryDerived(queryClient);
}

function warnSaveFailed(port: DetailPort, error: Error, body: CreateTrackRequest): void {
  port.warn('[detail] save track failed', {
    title: body.title,
    artist: body.artist,
    error: error.message,
    retryable: isRetryable(error),
    corr_id: correlationIdOf(error),
  });
}

function markFailed(trackId: TrackId, error: Error): void {
  patchTrackStatus(trackId, failedTrackStatus(error), 'response');
}

function settleFailed(
  port: DetailPort,
  queryClient: QueryClient,
  error: Error,
  body: CreateTrackRequest,
  context: SaveContext,
): void {
  warnSaveFailed(port, error, body);
  removeTrackFromCaches(queryClient, context.optimisticId);
  markFailed(context.optimisticId, error);
}

export function useSaveTrack(): SaveTrack {
  const queryClient = useQueryClient();
  const handoff = useDetailHandoff();
  const port = useDetailPort();

  const mutation = useAppMutation({
    ...guardedMutationOptions<TrackResponse, CreateTrackRequest, SaveContext>({
      mutationFn: (body) => sendSave(port, body),
      onMutate: (body) => placeOptimistic(queryClient, body),
      onSuccess: (saved, body, context) => {
        adoptSaved(queryClient, saved, context);
        recordLibraryAdd(body, handoff);
      },
      onError: (error, body, context) => settleFailed(port, queryClient, error, body, context),
    }),
    action: 'detail.save_track',
    trackIdOf: optimisticTrackId,
  });

  return {
    mutate: mutation.mutate,
    mutateAsync: mutation.mutateAsync,
    isPending: mutation.isPending,
    failure: saveFailure(mutation.error, mutation.variables),
  };
}

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
  return {
    copy: { title: REMEMBERED_FAILURE_TITLE, body: owned.failureMessage },
    isRetryable: owned.retryable,
  };
}

function deriveState(
  canSave: boolean,
  save: { failure: SaveFailure | null; isPending: boolean },
  owned: OwnedTrack | null,
): SaveState {
  if (!canSave) return 'disabled';
  if (save.failure !== null) return failureState(save.failure);
  if (save.isPending) return 'saving';
  const remembered = rememberedFailure(owned);
  if (remembered !== null) return failureState(remembered);
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
    try {
      action({ onSettled: () => void (held.current = false) });
    } catch (error) {
      held.current = false;
      throw error;
    }
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
  const failure =
    save.failure ?? (state === 'failed' || state === 'rejected' ? rememberedFailure(owned) : null);
  const onSave = buildOnSave(state, { owned, track, save, retry }, guarded);
  return { state, failure, onSave };
}
