import { Alert } from 'react-native';
import { useMutation, useQueryClient, type QueryClient } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { deleteTrack } from '@shared/api-client/tracks';
import { forgetTrack } from '@shared/events/forgetTrack';
import {
  captureTrackPlacements,
  invalidateLibraryDerived,
  restoreTrackPlacements,
  type TrackCachePlacement,
} from '@shared/events/trackCachePatch';
import { usePinnedStore } from '@shared/offline/pinnedStore';
import {
  patchTrackStatus,
  useTrackStatusStore,
  type TrackStatus,
} from '@shared/acquisition/trackStatusStore';
import { guardedMutationOptions } from '@shared/session/signOutCleanup';

import { logTrackMutationFailure } from './logTrackMutationFailure';
import { classifyLibraryError, failureTail } from '../state';

type RemovedTrack = { placements: TrackCachePlacement[]; status: TrackStatus | undefined };

const deleteEndpoint = (trackId: TrackId) => `DELETE /v1/tracks/${trackId}`;

function unpin(trackId: TrackId): void {
  usePinnedStore.getState().unpin(trackId);
}

function removeOptimistically(queryClient: QueryClient) {
  return (trackId: TrackId): RemovedTrack => {
    const placements = captureTrackPlacements(queryClient, trackId);
    const status = useTrackStatusStore.getState().statuses[trackId];
    forgetTrack(queryClient, trackId);
    return { placements, status };
  };
}

function confirmRemoval(queryClient: QueryClient) {
  return (_deleted: void, trackId: TrackId): void => {
    unpin(trackId);
    invalidateLibraryDerived(queryClient);
  };
}

function putTrackBack(queryClient: QueryClient, trackId: TrackId, removed: RemovedTrack): void {
  restoreTrackPlacements(queryClient, removed.placements);
  if (removed.status) patchTrackStatus(trackId, removed.status);
}

function undoFailedRemoval(queryClient: QueryClient) {
  return (error: Error, trackId: TrackId, removed: RemovedTrack): void => {
    const failure = classifyLibraryError(error);
    if (failure === 'not-found') return unpin(trackId);
    logTrackMutationFailure('delete track', deleteEndpoint, trackId, error);
    putTrackBack(queryClient, trackId, removed);
    Alert.alert('Delete failed', `Could not remove the track. ${failureTail(failure)}`);
  };
}

function deleteTrackOptions(queryClient: QueryClient) {
  return guardedMutationOptions({
    mutationFn: (trackId: TrackId) => deleteTrack(trackId),
    onMutate: removeOptimistically(queryClient),
    onSuccess: confirmRemoval(queryClient),
    onError: undoFailedRemoval(queryClient),
  });
}

export function useDeleteTrack() {
  const queryClient = useQueryClient();
  return useMutation(deleteTrackOptions(queryClient));
}
