import { useLoopEnabled } from '@shared/killSwitch/killSwitchGate';

export function useDetailFetchEnabled(): boolean {
  return useLoopEnabled('detailEnrichment');
}
