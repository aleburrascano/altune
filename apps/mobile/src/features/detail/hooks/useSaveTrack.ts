/**
 * useSaveTrack — save the current track to the library with an optimistic UI.
 *
 * onMutate prepends a pending placeholder to the ['library-home'] snapshot (so
 * Save feels instant and the detail save-control + Activity Dock — which read
 * library-home — show feedback from anywhere); onSuccess swaps the placeholder
 * for the real server row so acquisition SSE events (real id) match it; onError
 * rolls back; onSettled invalidates so the authoritative server state
 * reconciles.
 *
 * The cache transforms live in ../save-cache (pure, unit-tested); this hook is
 * the React Query wiring shell.
 */

import { useMutation, useQueryClient } from '@tanstack/react-query';

import { createTrack } from '@shared/api-client/tracks';
import type { CreateTrackRequest, ListTracksResponse, TrackResponse } from '@shared/api-client/types';
import { getDetailHandoff, getDetailHandoffSearchId } from '@shared/lib/detail-handoff';
import { libraryKeys } from '@shared/lib/query-keys';
import { enqueueCritical } from '@shared/telemetry/outbox';

import {
  insertOptimisticTrackHome,
  optimisticTrack,
  replaceOptimisticTrackHome,
} from '../save-cache';

type SaveContext = {
  previousHome: ListTracksResponse | undefined;
  optimisticId: string;
};

export function useSaveTrack() {
  const queryClient = useQueryClient();

  return useMutation<TrackResponse, Error, CreateTrackRequest, SaveContext>({
    mutationFn: (body) => createTrack(body),
    onMutate: async (body) => {
      await queryClient.cancelQueries({ queryKey: libraryKeys.home });
      const previousHome = queryClient.getQueryData<ListTracksResponse>(libraryKeys.home);
      const placeholder = optimisticTrack(body, new Date().toISOString());
      queryClient.setQueryData<ListTracksResponse>(libraryKeys.home, (data) =>
        insertOptimisticTrackHome(data, placeholder),
      );
      return { previousHome, optimisticId: placeholder.id };
    },
    onSuccess: (data, body, context) => {
      // Swap the placeholder for the real row so acquisition events (real id) match.
      queryClient.setQueryData<ListTracksResponse>(libraryKeys.home, (prev) =>
        replaceOptimisticTrackHome(prev, context.optimisticId, data),
      );
      // library_add is the positive label for the self-growing corpus, so it
      // carries the originating search_id + result_signature when the save came
      // from a search-tapped result (both null for a library-originated save).
      // Label-critical → routed through the outbox (idempotency key + retry).
      const handoff = getDetailHandoff();
      void enqueueCritical({
        type: 'library_add',
        search_id: getDetailHandoffSearchId() ?? undefined,
        payload: {
          title: body.title,
          artist: body.artist,
          album: body.album,
          year: body.year,
          result_signature: handoff?.result_signature ?? null,
        },
      });
    },
    onError: (_error, _body, context) => {
      queryClient.setQueryData(libraryKeys.home, context?.previousHome);
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: libraryKeys.home });
    },
  });
}
