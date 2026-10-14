import { useGatedCallback, useLoopEnabled } from '@shared/killSwitch/killSwitchGate';

export function useDiscoverFetchEnabled(): boolean {
  return useLoopEnabled('discovery');
}

export function useGatedDiscoverCall(action: () => void | Promise<unknown>): () => void {
  return useGatedCallback('discovery', action);
}
