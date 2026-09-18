import type { TrackId } from '@shared/api-client/ids';

import { failureLogFields } from '../failureLogFields';

/**
 * The one diagnostic line a failed track mutation leaves behind. Redacted like
 * `apiFetch`'s own `logFailure` one layer below: `failureLogFields` keeps the
 * caught error out, and an endpoint's query string is stripped here.
 */
export function logTrackMutationFailure(
  action: string,
  endpoint: (trackId: TrackId) => string,
  trackId: TrackId,
  error: unknown,
): void {
  console.warn(`[library] ${action} failed`, {
    trackId,
    endpoint: endpoint(trackId).split('?')[0],
    ...failureLogFields(error),
  });
}
