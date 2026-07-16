/**
 * sessionExpired — the one feedback edge from HTTP 401 back to auth state.
 *
 * The Supabase SDK is the sole authority on session validity, and it only knows
 * what it can see locally. When the backend rejects a token the SDK still holds
 * (revoked server-side, JWKS rotation, clock skew), no SIGNED_OUT is emitted:
 * AuthGate keeps rendering the app and every query 401s forever with no way out
 * but hunting down Sign Out in Settings. apiFetch — the single choke point for
 * every /v1 call — marks this flag on a server 401, and AuthGate reads it to
 * offer an explicit re-auth.
 *
 * AIDEV-DECISION: marking is deliberately NOT the same as acting. We do not
 * auto-sign-out on 401: a server-side JWKS/config blip would then bounce every
 * user mid-session at once. The user chooses when to re-authenticate. (Silent
 * session resurrection on refresh failure is out of scope — see
 * docs/specs/auth-hardening/spec.md.)
 *
 * A module-level store rather than context: the producer (apiFetch) lives
 * outside the React tree.
 */
import { useSyncExternalStore } from 'react';

let expired = false;
const listeners = new Set<() => void>();

function emit(): void {
  for (const listener of listeners) listener();
}

/** Called by apiFetch when the backend rejects a token the SDK considers live. */
export function markSessionExpired(): void {
  if (expired) return;
  expired = true;
  emit();
}

/** Called when identity changes (sign-out / user switch) — the flag is stale. */
export function clearSessionExpired(): void {
  if (!expired) return;
  expired = false;
  emit();
}

export function getSessionExpired(): boolean {
  return expired;
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
