import type { TrackId } from '@shared/api-client/ids';

import { failureLogFields } from '../failureLogFields';

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
