import { useGatedCallback, useLoopEnabled } from '@shared/killSwitch/killSwitchGate';

/** Whether this screen's enrichment and discovery fetches may fire (#1666). */
export function useDetailFetchEnabled(): boolean {
  return useLoopEnabled('detailEnrichment');
}

/**
 * Wraps a section's retry affordance in the same switch. react-query's `refetch` fetches whatever
 * `enabled` says, so without this a tap on "try again" would reach a provider the operator has
 * switched off — the one endpoint the switch exists to protect.
 */
export function useGatedRefetch(refetch: () => unknown): () => void {
  return useGatedCallback('detailEnrichment', refetch);
}
