/**
 * useSaveTrack — save the current track to the library with an optimistic UI.
 *
 * onMutate prepends a pending placeholder to BOTH the ['library'] infinite cache
 * and the ['library-home'] snapshot (so Save feels instant *and* the detail
 * save-control + Activity Dock — which read library-home first — show feedback
 * from anywhere); onSuccess swaps the placeholder for the real server row so
 * acquisition SSE events (real id) match it; onError rolls back; onSettled
 * invalidates so the authoritative server state reconciles.
 *
 * The cache transforms live in ../save-cache (pure, unit-tested); this hook is
 * the React Query wiring shell.
 */

import { useMutation, useQueryClient } from '@tanstack/react-query';
import type { InfiniteData } from '@tanstack/react-query';

import { createTrack } from '@shared/api-client/tracks';
import type { CreateTrackRequest, ListTracksResponse, TrackResponse } from '@shared/api-client/types';
import { getDetailHandoff, getDetailHandoffSearchId } from '@shared/lib/detail-handoff';
import { enqueueCritical } from '@shared/telemetry/outbox';

import {
  insertOptimisticTrack,
  insertOptimisticTrackHome,
  optimisticTrack,
  replaceOptimisticTrackHome,
  replaceOptimisticTrackInfinite,
} from '../save-cache';

const LIBRARY_KEY = ['library'] as const;
const LIBRARY_HOME_KEY = ['library-home'] as const;

type LibraryData = InfiniteData<ListTracksResponse>;
type SaveContext = {
  previous: LibraryData | undefined;
  previousHome: ListTracksResponse | undefined;
  optimisticId: string;
};

export function useSaveTrack() {
  const queryClient = useQueryClient();

  return useMutation<TrackResponse, Error, CreateTrackRequest, SaveContext>({
    mutationFn: (body) => createTrack(body),
    onMutate: async (body) => {
      await queryClient.cancelQueries({ queryKey: LIBRARY_KEY });
      await queryClient.cancelQueries({ queryKey: LIBRARY_HOME_KEY });
      const previous = queryClient.getQueryData<LibraryData>(LIBRARY_KEY);
      const previousHome = queryClient.getQueryData<ListTracksResponse>(LIBRARY_HOME_KEY);
      const placeholder = optimisticTrack(body, new Date().toISOString());
      queryClient.setQueryData<LibraryData>(LIBRARY_KEY, (data) =>
        insertOptimisticTrack(data, placeholder),
      );
      queryClient.setQueryData<ListTracksResponse>(LIBRARY_HOME_KEY, (data) =>
        insertOptimisticTrackHome(data, placeholder),
      );
      return { previous, previousHome, optimisticId: placeholder.id };
    },
    onSuccess: (data, body, context) => {
      // Swap the placeholder for the real row so acquisition events (real id) match.
      queryClient.setQueryData<LibraryData>(LIBRARY_KEY, (prev) =>
        replaceOptimisticTrackInfinite(prev, context.optimisticId, data),
      );
      queryClient.setQueryData<ListTracksResponse>(LIBRARY_HOME_KEY, (prev) =>
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
      queryClient.setQueryData(LIBRARY_KEY, context?.previous);
      queryClient.setQueryData(LIBRARY_HOME_KEY, context?.previousHome);
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: LIBRARY_KEY });
      void queryClient.invalidateQueries({ queryKey: LIBRARY_HOME_KEY });
    },
  });
}
