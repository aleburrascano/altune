/**
 * useSaveTrack — save the current track to the library with an optimistic UI.
 *
 * onMutate prepends a pending placeholder to the ['library'] cache (so the
 * Save feels instant); onError rolls back to the pre-mutation snapshot; onSettled
 * invalidates ['library'] so the authoritative server state (including a dedup
 * hit that returns the existing row) reconciles the optimistic placeholder.
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

import { insertOptimisticTrack, optimisticTrack } from '../save-cache';

const LIBRARY_KEY = ['library'] as const;

type LibraryData = InfiniteData<ListTracksResponse>;
type SaveContext = { previous: LibraryData | undefined };

export function useSaveTrack() {
  const queryClient = useQueryClient();

  return useMutation<TrackResponse, Error, CreateTrackRequest, SaveContext>({
    mutationFn: (body) => createTrack(body),
    onSuccess: (_data, body) => {
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
    onMutate: async (body) => {
      await queryClient.cancelQueries({ queryKey: LIBRARY_KEY });
      const previous = queryClient.getQueryData<LibraryData>(LIBRARY_KEY);
      const placeholder = optimisticTrack(body, new Date().toISOString());
      queryClient.setQueryData<LibraryData>(LIBRARY_KEY, (data) =>
        insertOptimisticTrack(data, placeholder),
      );
      return { previous };
    },
    onError: (_error, _body, context) => {
      queryClient.setQueryData(LIBRARY_KEY, context?.previous);
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: LIBRARY_KEY });
      void queryClient.invalidateQueries({ queryKey: ['library-home'] });
    },
  });
}
