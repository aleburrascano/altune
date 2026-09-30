import { useQueryClient, type QueryClient, type UseMutationResult } from '@tanstack/react-query';

import { isRetryable } from '@shared/errors';
import type { TrackId } from '@shared/api-client/ids';
import { createTrack } from '@shared/api-client/tracks';
import { acquisitionOf, toTrackStatus } from '@shared/api-client/trackAcquisition';
import type { CreateTrackRequest, TrackResponse } from '@shared/api-client/types';
import { rememberDownloadMeta } from '@shared/acquisition/downloadStore';
import {
  linkTrackIdentity,
  patchTrackStatus,
  removeTrackStatus,
  trackIdentityKey,
} from '@shared/acquisition/trackStatusStore';
import {
  invalidateLibraryDerived,
  removeTrackFromCaches,
  replaceTrackInCaches,
  upsertTrackInCaches,
} from '@shared/events/trackCachePatch';
import { useAppMutation } from '@shared/query/useAppMutation';
import { guardedMutationOptions } from '@shared/session/signOutCleanup';
import { enqueueCritical } from '@shared/telemetry/outbox';

import { useDetailHandoff } from '../handoff-context';
import { optimisticTrack, optimisticTrackId, saveIdempotencyKey } from '../save-cache';

type SaveContext = { optimisticId: TrackId; identity: string | null };

type SaveMutation = UseMutationResult<TrackResponse, Error, CreateTrackRequest, SaveContext>;

export type SaveFailure = {
  message: string;
  isRetryable: boolean;
  trackId?: TrackId;
};

export type SaveTrack = {
  mutate: SaveMutation['mutate'];
  mutateAsync: SaveMutation['mutateAsync'];
  isPending: boolean;
  failure: SaveFailure | null;
};

type SaveBody = CreateTrackRequest | undefined;

function saveFailure(error: Error | null, body: SaveBody): SaveFailure | null {
  if (error === null) return null;
  return {
    message: error.message,
    isRetryable: isRetryable(error),
    ...(body === undefined ? {} : { trackId: optimisticTrackId(body) }),
  };
}

type Handoff = ReturnType<typeof useDetailHandoff>;

function sendSave(body: CreateTrackRequest): Promise<TrackResponse> {
  return createTrack(body, saveIdempotencyKey(body)).then((saved) => {
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

function adoptSaved(queryClient: QueryClient, saved: TrackResponse, context: SaveContext): void {
  replaceTrackInCaches(queryClient, context.optimisticId, saved);
  removeTrackStatus(context.optimisticId);
  patchTrackStatus(saved.id, toTrackStatus(acquisitionOf(saved)));
  linkTrackIdentity(context.identity, saved.id);
  invalidateLibraryDerived(queryClient);
}

function warnSaveFailed(error: Error, body: CreateTrackRequest): void {
  console.warn('[detail] save track failed', {
    title: body.title,
    artist: body.artist,
    error: error.message,
    retryable: isRetryable(error),
  });
}

function markFailed(trackId: TrackId, error: Error): void {
  const failed = { acquisitionStatus: 'failed', failureMessage: error.message } as const;
  patchTrackStatus(trackId, failed, 'response');
}

function settleFailed(
  queryClient: QueryClient,
  error: Error,
  body: CreateTrackRequest,
  context: SaveContext,
): void {
  warnSaveFailed(error, body);
  removeTrackFromCaches(queryClient, context.optimisticId);
  markFailed(context.optimisticId, error);
}

export function useSaveTrack(): SaveTrack {
  const queryClient = useQueryClient();
  const handoff = useDetailHandoff();

  const mutation = useAppMutation({
    ...guardedMutationOptions<TrackResponse, CreateTrackRequest, SaveContext>({
      mutationFn: sendSave,
      onMutate: (body) => placeOptimistic(queryClient, body),
      onSuccess: (saved, body, context) => {
        adoptSaved(queryClient, saved, context);
        recordLibraryAdd(body, handoff);
      },
      onError: (error, body, context) => settleFailed(queryClient, error, body, context),
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
