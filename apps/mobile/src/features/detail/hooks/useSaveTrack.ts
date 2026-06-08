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

import { insertOptimisticTrack, optimisticTrack } from '../save-cache';

const LIBRARY_KEY = ['library'] as const;

type LibraryData = InfiniteData<ListTracksResponse>;
type SaveContext = { previous: LibraryData | undefined };

export function useSaveTrack() {
  const queryClient = useQueryClient();

  return useMutation<TrackResponse, Error, CreateTrackRequest, SaveContext>({
    mutationFn: (body) => createTrack(body),
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
    },
  });
}
