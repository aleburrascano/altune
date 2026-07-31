import { useSyncExternalStore } from 'react';

let expired = false;
const listeners = new Set<() => void>();

function emit(): void {
  for (const listener of listeners) listener();
}

export function markSessionExpired(): void {
  if (expired) return;
  expired = true;
  emit();
}

export function clearSessionExpired(): void {
  if (!expired) return;
  expired = false;
  emit();
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
