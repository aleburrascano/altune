import { ApiError } from '@shared/api-client/errors';
import type { TrackId } from '@shared/api-client/ids';

/**
 * The one diagnostic line a failed track mutation leaves behind: the action, the
 * track, the endpoint it hit, the HTTP status when the API answered, and the real
 * error (stack included). The user only ever sees a generic Alert, so this is what
 * a production failure is triaged from.
 */
export function logTrackMutationFailure(
  action: string,
  endpoint: (trackId: TrackId) => string,
  trackId: TrackId,
  error: unknown,
): void {
  console.warn(`[library] ${action} failed`, {
    trackId,
    endpoint: endpoint(trackId),
    status: error instanceof ApiError ? error.status : undefined,
    error,
  });
}
