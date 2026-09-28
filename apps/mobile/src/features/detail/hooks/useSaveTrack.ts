import { useMutation, useQueryClient, type UseMutationResult } from '@tanstack/react-query';

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

export function useSaveTrack(): SaveTrack {
  const queryClient = useQueryClient();
  const handoff = useDetailHandoff();

  const mutation = useMutation<TrackResponse, Error, CreateTrackRequest, SaveContext>({
    mutationFn: (body) =>
      createTrack(body, saveIdempotencyKey(body)).then((saved) => {
        rememberDownloadMeta(saved.id, {
          title: saved.title,
          artist: saved.artist,
          artworkUrl: saved.artwork_url,
        });
        return saved;
      }),
    onMutate: (body) => {
      const placeholder = optimisticTrack(body, new Date().toISOString());
      upsertTrackInCaches(queryClient, placeholder);
      const identity = trackIdentityKey(body.title, body.artist);
      patchTrackStatus(
        placeholder.id,
        { acquisitionStatus: 'pending', failureMessage: null },
        'optimistic',
      );
      linkTrackIdentity(identity, placeholder.id);
      return { optimisticId: placeholder.id, identity };
    },
    onSuccess: (data, body, context) => {
      replaceTrackInCaches(queryClient, context.optimisticId, data);
      removeTrackStatus(context.optimisticId);
      patchTrackStatus(data.id, toTrackStatus(acquisitionOf(data)));
      linkTrackIdentity(context.identity, data.id);
      invalidateLibraryDerived(queryClient);

      void enqueueCritical({
        type: 'library_add',
        search_id: handoff?.searchId ?? undefined,
        payload: {
          title: body.title,
          artist: body.artist,
          album: body.album,
          year: body.year,
          ...(handoff?.result.result_signature != null
            ? { result_signature: handoff.result.result_signature }
            : {}),
        },
      });
    },
    onError: (error, body, context) => {
      console.warn('[detail] save track failed', {
        title: body.title,
        artist: body.artist,
        error: error.message,
        retryable: isRetryable(error),
      });
      if (context) {
        removeTrackFromCaches(queryClient, context.optimisticId);
        patchTrackStatus(
          context.optimisticId,
          { acquisitionStatus: 'failed', failureMessage: error.message },
          'response',
        );
      }
    },
  });

  return {
    mutate: mutation.mutate,
    mutateAsync: mutation.mutateAsync,
    isPending: mutation.isPending,
    failure: saveFailure(mutation.error, mutation.variables),
  };
}
