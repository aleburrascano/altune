import { useQueryClient, type QueryClient } from '@tanstack/react-query';

import { useAppMutation } from '@shared/query/useAppMutation';

import type { TrackId } from '@shared/api-client/ids';
import { toPending } from '@shared/api-client/trackAcquisition';
import { reacquireTrack } from '@shared/api-client/tracks';
import { patchTrackInCaches } from '@shared/events/trackCachePatch';
import { guardedMutationOptions } from '@shared/session/signOutCleanup';

import { dropVanishedTrack } from './dropVanishedTrack';
import { logTrackMutationFailure } from './logTrackMutationFailure';
import { trackMutationKeys, useOneRunPerTrack, type TrackMutation } from './useOneRunPerTrack';
import { alertLibraryFailure } from '../libraryFailureAlert';
import { classifyLibraryError } from '../state';

const reacquireEndpoint = (trackId: TrackId) => `POST /v1/tracks/${trackId}/reacquire`;

const REACQUIRE_FAILED = 'Could not start a re-acquisition. Your current audio is unchanged.';

function markPending(queryClient: QueryClient) {
  return (_started: void, trackId: TrackId): void => {
    patchTrackInCaches(queryClient, trackId, toPending());
  };
}

function reportFailedReacquire(queryClient: QueryClient) {
  return (error: Error, trackId: TrackId): void => {
    const failure = classifyLibraryError(error);
    if (failure === 'not-found') return dropVanishedTrack(queryClient, trackId);
    logTrackMutationFailure('re-acquire track', reacquireEndpoint, trackId, error);
    alertLibraryFailure('library.reacquire', 'Re-acquire failed', REACQUIRE_FAILED, failure);
  };
}

function reacquireOptions(queryClient: QueryClient) {
  return guardedMutationOptions({
    mutationFn: (trackId: TrackId) => reacquireTrack(trackId),
    onSuccess: markPending(queryClient),
    onError: reportFailedReacquire(queryClient),
  });
}

export function useReacquireTrack(): TrackMutation<void, unknown> {
  const queryClient = useQueryClient();
  const mutation = useAppMutation({
    action: 'library.reacquire',
    trackIdOf: (trackId: TrackId) => trackId,
    mutationKey: trackMutationKeys.reacquire,
    ...reacquireOptions(queryClient),
  });
  return useOneRunPerTrack(mutation, trackMutationKeys.reacquire);
}
