import { useMutation, useQueryClient, type UseMutationResult } from '@tanstack/react-query';

import { createTrack } from '@shared/api-client/tracks';
import type { CreateTrackRequest, TrackResponse } from '@shared/api-client/types';
import {
  linkTrackIdentity,
  patchTrackStatus,
  removeTrackStatus,
  trackIdentityKey,
} from '@shared/acquisition/trackStatusStore';
import {
  removeTrackFromCaches,
  replaceTrackInCaches,
  upsertTrackInCaches,
} from '@shared/events/trackCachePatch';
import { libraryKeys } from '@shared/lib/query-keys';
import { enqueueCritical } from '@shared/telemetry/outbox';

import { useDetailHandoff } from '../handoff-context';
import { optimisticTrack } from '../save-cache';

type SaveContext = { optimisticId: string; identity: string | null };

type SaveMutation = UseMutationResult<TrackResponse, Error, CreateTrackRequest, SaveContext>;

// Narrow view over the TanStack mutation: only what save consumers actually
// use, so callers can't reach for the other ~12 members of UseMutationResult.
export type SaveTrack = {
  mutate: SaveMutation['mutate'];
  mutateAsync: SaveMutation['mutateAsync'];
  isPending: boolean;
  isError: boolean;
};

export function useSaveTrack(): SaveTrack {
  const queryClient = useQueryClient();
  const handoff = useDetailHandoff();

  const mutation = useMutation<TrackResponse, Error, CreateTrackRequest, SaveContext>({
    mutationFn: (body) => createTrack(body),
    onMutate: (body) => {
      const placeholder = optimisticTrack(body, new Date().toISOString());
      upsertTrackInCaches(queryClient, placeholder);
      const identity = trackIdentityKey(body.title, body.artist);
      patchTrackStatus(placeholder.id, { acquisitionStatus: 'pending', failureMessage: null });
      linkTrackIdentity(identity, placeholder.id);
      return { optimisticId: placeholder.id, identity };
    },
    onSuccess: (data, body, context) => {
      replaceTrackInCaches(queryClient, context.optimisticId, data);
      removeTrackStatus(context.optimisticId);
      patchTrackStatus(data.id, {
        acquisitionStatus: data.acquisition_status,
        failureMessage: data.failure_message ?? null,
      });
      linkTrackIdentity(context.identity, data.id);
      void queryClient.invalidateQueries({ queryKey: libraryKeys.albumsPrefix });
      void queryClient.invalidateQueries({ queryKey: libraryKeys.artistsPrefix });
      void queryClient.invalidateQueries({ queryKey: libraryKeys.lookupPrefix });

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
      // without a live repro; the UI only sees a generic failed state.
      console.warn('[detail] save track failed', {
        title: body.title,
        artist: body.artist,
        error: error.message,
      });
      if (context) {
        // The POST never landed, so drop the optimistic library row. Keep the
        // per-track status linked to its identity and mark it failed instead of
        // wiping it, so the row's save control shows a visible failure/retry
        // state rather than silently reverting to "add".
        removeTrackFromCaches(queryClient, context.optimisticId);
        patchTrackStatus(context.optimisticId, {
          acquisitionStatus: 'failed',
          failureMessage: error.message,
        });
      }
    },
  });

  return {
    mutate: mutation.mutate,
    mutateAsync: mutation.mutateAsync,
    isPending: mutation.isPending,
    isError: mutation.isError,
  };
}
