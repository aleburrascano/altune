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

// Why the save failed, in the two terms the UI branches on. `isRetryable` is the
// app's one transient/permanent classifier — the same one the QueryClient's retry
// policy uses — so a permanent refusal is never offered as a retry (#1661).
export type SaveFailure = {
  message: string;
  isRetryable: boolean;
  trackId?: TrackId;
};

// Narrow view over the TanStack mutation: only what save consumers actually
// use, so callers can't reach for the other ~12 members of UseMutationResult.
export type SaveTrack = {
  mutate: SaveMutation['mutate'];
  mutateAsync: SaveMutation['mutateAsync'];
  isPending: boolean;
  failure: SaveFailure | null;
};

function saveFailure(error: Error | null, body: CreateTrackRequest | undefined): SaveFailure | null {
  if (error === null) {
    return null;
  }
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
      // The save POST failed. Log the actual reason plus the track identity so a
      // real incident (a provider/API outage) can be told apart from a one-off
      // without a live repro, and the classification the UI acted on with it.
      console.warn('[detail] save track failed', {
        title: body.title,
        artist: body.artist,
        error: error.message,
        retryable: isRetryable(error),
      });
      if (context) {
        // The POST never landed, so drop the optimistic library row. Keep the
        // per-track status linked to its identity and mark it failed instead of
        // wiping it, so the row's save control shows a visible failure/retry
        // state rather than silently reverting to "add".
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
