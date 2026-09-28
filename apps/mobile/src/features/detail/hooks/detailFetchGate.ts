import { useGatedCallback, useLoopEnabled } from '@shared/killSwitch/killSwitchGate';

export function useDetailFetchEnabled(): boolean {
  return useLoopEnabled('detailEnrichment');
}

export function useGatedRefetch(refetch: () => unknown): () => void {
  return useGatedCallback('detailEnrichment', refetch);
}
