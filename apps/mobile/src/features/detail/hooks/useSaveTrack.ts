import { useMutation, useQueryClient } from '@tanstack/react-query';

import { createTrack } from '@shared/api-client/tracks';
import type { CreateTrackRequest, TrackResponse } from '@shared/api-client/types';
import { patchTrackStatus, removeTrackStatus } from '@shared/acquisition/trackStatusStore';
import {
  removeTrackFromCaches,
  replaceTrackInCaches,
  upsertTrackInCaches,
} from '@shared/events/trackCachePatch';
import { getDetailHandoff, getDetailHandoffSearchId } from '@shared/lib/detail-handoff';
import { libraryKeys } from '@shared/lib/query-keys';
import { enqueueCritical } from '@shared/telemetry/outbox';

import { optimisticTrack } from '../save-cache';

type SaveContext = { optimisticId: string };

export function useSaveTrack() {
  const queryClient = useQueryClient();

  return useMutation<TrackResponse, Error, CreateTrackRequest, SaveContext>({
    mutationFn: (body) => createTrack(body),
    onMutate: (body) => {
      const placeholder = optimisticTrack(body, new Date().toISOString());
      upsertTrackInCaches(queryClient, placeholder);
      return { optimisticId: placeholder.id };
    },
    onSuccess: (data, body, context) => {
      replaceTrackInCaches(queryClient, context.optimisticId, data);
      patchTrackStatus(data.id, {
        acquisitionStatus: data.acquisition_status,
        failureMessage: data.failure_message ?? null,
      });
      void queryClient.invalidateQueries({ queryKey: libraryKeys.albumsPrefix });
      void queryClient.invalidateQueries({ queryKey: libraryKeys.artistsPrefix });
      void queryClient.invalidateQueries({ queryKey: libraryKeys.lookupPrefix });

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
      if (context) {
        removeTrackFromCaches(queryClient, context.optimisticId);
        removeTrackStatus(context.optimisticId);
      }
    },
  });
}
