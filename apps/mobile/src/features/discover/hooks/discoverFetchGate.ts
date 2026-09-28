import { useGatedCallback, useLoopEnabled } from '@shared/killSwitch/killSwitchGate';

export function useDiscoverFetchEnabled(): boolean {
  return useLoopEnabled('discovery');
}

export function useGatedDiscoverCall(action: () => unknown): () => void {
  return useGatedCallback('discovery', action);
}
