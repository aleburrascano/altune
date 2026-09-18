import type { TrackId } from '@shared/api-client/ids';

type TrackMutationState = { isPending: boolean; variables: TrackId | undefined };

/**
 * The one owner of "which track this mutation is acting on". A settled mutation keeps the
 * last `variables` it ran with, so the id is only meaningful while pending — screens asking
 * the boolean question compare against this rather than re-deriving the rule (#1694).
 */
export function activeMutationId(mutation: TrackMutationState): TrackId | undefined {
  return mutation.isPending ? mutation.variables : undefined;
}
