import { useSyncExternalStore } from 'react';

import { currentSessionEpoch, isSameSession } from '@shared/session/signOutCleanup';

export type CredentialStamp = { readonly epoch: number; readonly renewal: number };

let expired = false;
let renewal = 0;
const listeners = new Set<() => void>();

function emit(): void {
  for (const listener of listeners) listener();
}

export function stampCredentials(): CredentialStamp {
  return { epoch: currentSessionEpoch(), renewal };
}

function holdsCurrentCredentials(stamp: CredentialStamp): boolean {
  return isSameSession(stamp.epoch) && stamp.renewal === renewal;
}

export function markSessionExpired(sentWith: CredentialStamp = stampCredentials()): void {
  if (expired || !holdsCurrentCredentials(sentWith)) return;
  expired = true;
  emit();
}

export function clearSessionExpired(): void {
  if (!expired) return;
  expired = false;
  emit();
}

export function renewSessionCredentials(): void {
  renewal += 1;
  clearSessionExpired();
}

export function getSessionExpired(): boolean {
  return expired;
}

export function _listenerCountForTest(): number {
  return listeners.size;
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function useSessionExpired(): boolean {
  return useSyncExternalStore(subscribe, getSessionExpired, getSessionExpired);
}
