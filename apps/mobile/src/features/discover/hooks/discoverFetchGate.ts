import { useGatedCallback, useLoopEnabled } from '@shared/killSwitch/killSwitchGate';

/** Whether discover's search, suggest, history and clear-history calls may fire (#1685). */
export function useDiscoverFetchEnabled(): boolean {
  return useLoopEnabled('discovery');
}

/** Wraps an affordance that calls the discovery backend outside a query's `enabled` in the switch. */
export function useGatedDiscoverCall(action: () => unknown): () => void {
  return useGatedCallback('discovery', action);
}
