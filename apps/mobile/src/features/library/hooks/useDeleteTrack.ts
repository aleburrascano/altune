import { useQueryClient, type QueryClient } from '@tanstack/react-query';

import { useAppMutation } from '@shared/query/useAppMutation';

import type { TrackId } from '@shared/api-client/ids';
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

import { useLibraryPort } from '@features/library/ports/LibraryPortProvider';
import type { LibraryPort } from '@features/library/ports/libraryPort';

import { deleteOne } from '@features/library/hooks/useDeleteTracks';
import { logTrackMutationFailure } from '@features/library/hooks/logTrackMutationFailure';
import { alertLibraryFailure } from '@features/library/libraryFailureAlert';
import { classifyLibraryError } from '@features/library/domain/state';

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
    alertLibraryFailure(
      'library.delete_track',
      'Delete failed',
      'Could not remove the track.',
      failure,
    );
  };
}

function deleteTrackOptions(queryClient: QueryClient, port: LibraryPort) {
  return guardedMutationOptions({
    mutationFn: (trackId: TrackId) => deleteOne(port, trackId),
    onMutate: removeOptimistically(queryClient),
    onSuccess: confirmRemoval(queryClient),
    onError: undoFailedRemoval(queryClient),
  });
}

export function useDeleteTrack() {
  const queryClient = useQueryClient();
  const port = useLibraryPort();
  return useAppMutation({
    ...deleteTrackOptions(queryClient, port),
    action: 'library.delete_track',
    trackIdOf: (trackId: TrackId) => trackId,
  });
}
