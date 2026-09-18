import { useSyncExternalStore } from 'react';

import { isLoopEnabled, onKillSwitchChange } from '@shared/killSwitch/killSwitch';

function subscribeToDetailSwitch(onFlip: () => void): () => void {
  return onKillSwitchChange((loop) => {
    if (loop === 'detailEnrichment') onFlip();
  });
}

function isDetailFetchEnabled(): boolean {
  return isLoopEnabled('detailEnrichment');
}

/**
 * Whether this screen's enrichment and discovery fetches may fire (#1666). Re-renders the caller
 * when the remote switch flips, so turning it off stops the queries on screens already mounted and
 * turning it back on lets them run again without a relaunch.
 */
export function useDetailFetchEnabled(): boolean {
  return useSyncExternalStore(subscribeToDetailSwitch, isDetailFetchEnabled, isDetailFetchEnabled);
}

/**
 * Wraps a section's retry affordance in the same switch. react-query's `refetch` fetches whatever
 * `enabled` says, so without this a tap on "try again" would reach a provider the operator has
 * switched off — the one endpoint the switch exists to protect.
 */
export function useGatedRefetch(refetch: () => unknown): () => void {
  const isFetchEnabled = useDetailFetchEnabled();
  return () => {
    if (isFetchEnabled) void refetch();
  };
}
