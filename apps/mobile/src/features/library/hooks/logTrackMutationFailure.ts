import { ApiError, correlationIdOf } from '@shared/api-client/errors';
import type { TrackId } from '@shared/api-client/ids';

import { classifyLibraryError } from '../state';

function apiErrorFields(error: unknown): { status?: number; code?: string } {
  if (!(error instanceof ApiError)) return {};
  return { status: error.status, ...(error.code === undefined ? {} : { code: error.code }) };
}

/**
 * The one diagnostic line a failed track mutation leaves behind. Redacted like
 * `apiFetch`'s own `logFailure`, one layer below: never the caught error itself
 * (its message can carry a server message or request data, its stack the local
 * paths), and never an endpoint's query string. The correlation id matches the
 * server's log lines, so triage reads the message there, not here.
 */
export function logTrackMutationFailure(
  action: string,
  endpoint: (trackId: TrackId) => string,
  trackId: TrackId,
  error: unknown,
): void {
  const correlationId = correlationIdOf(error);
  console.warn(`[library] ${action} failed`, {
    trackId,
    endpoint: endpoint(trackId).split('?')[0],
    ...apiErrorFields(error),
    failure: classifyLibraryError(error),
    ...(correlationId === undefined ? {} : { correlationId }),
  });
}
