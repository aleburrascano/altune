import { useGatedCallback } from '@shared/killSwitch/killSwitchGate';

export function useGatedRefetch(refetch: () => unknown): () => void {
  return useGatedCallback('detailEnrichment', refetch);
}
