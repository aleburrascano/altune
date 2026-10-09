import { useQueryClient, type QueryClient } from '@tanstack/react-query';

import { useAppMutation } from '@shared/query/useAppMutation';

import { forgetTrack } from '@shared/events/forgetTrack';
import { showFailureAlert } from '@shared/ui';
import type { TrackId } from '@shared/api-client/ids';
import { toPending } from '@shared/api-client/trackAcquisition';
import { patchTrackInCaches } from '@shared/events/trackCachePatch';
import { guardedMutationOptions } from '@shared/session/signOutCleanup';

import { useLibraryPort } from '@features/library/ports/LibraryPortProvider';
import type { LibraryPort } from '@features/library/ports/libraryPort';

import { trackNotFoundAlert } from '@features/library/domain/trackNotFoundAlert';
import { logTrackMutationFailure } from '@features/library/domain/logTrackMutationFailure';
import {
  trackMutationKeys,
  useOneRunPerTrack,
  type TrackMutation,
} from '@features/library/hooks/useOneRunPerTrack';
import { libraryFailureAlert } from '@features/library/domain/libraryFailureAlert';
import { classifyLibraryError } from '@features/library/domain/state';

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
    if (failure === 'not-found') {
      forgetTrack(queryClient, trackId);
      return showFailureAlert(trackNotFoundAlert(trackId));
    }
    logTrackMutationFailure('re-acquire track', reacquireEndpoint, trackId, error);
    showFailureAlert(
      libraryFailureAlert('library.reacquire', 'Re-acquire failed', REACQUIRE_FAILED, failure),
    );
  };
}

function reacquireOptions(queryClient: QueryClient, port: LibraryPort) {
  return guardedMutationOptions({
    mutationFn: (trackId: TrackId) => port.reacquireTrack(trackId),
    onSuccess: markPending(queryClient),
    onError: reportFailedReacquire(queryClient),
  });
}

export function useReacquireTrack(): TrackMutation<void, unknown> {
  const queryClient = useQueryClient();
  const port = useLibraryPort();
  const mutation = useAppMutation({
    action: 'library.reacquire',
    trackIdOf: (trackId: TrackId) => trackId,
    mutationKey: trackMutationKeys.reacquire,
    ...reacquireOptions(queryClient, port),
  });
  return useOneRunPerTrack(mutation, trackMutationKeys.reacquire);
}
