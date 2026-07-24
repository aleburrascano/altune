import { useMutation, useQueryClient } from '@tanstack/react-query';

import { createTrack } from '@shared/api-client/tracks';
import type {
  CreateTrackRequest,
  ListTracksResponse,
  TrackResponse,
} from '@shared/api-client/types';
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
      queryClient.setQueryData<ListTracksResponse>(libraryKeys.home, (prev) =>
        replaceOptimisticTrackHome(prev, context.optimisticId, data),
      );
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
