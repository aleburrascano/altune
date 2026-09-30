import { bumpSessionEpoch } from '@shared/session/sessionEpoch';

export { currentSessionEpoch, isSameSession } from '@shared/session/sessionEpoch';
export { guardedMutationOptions, SessionEndedError } from '@shared/session/guardedMutation';

export type SignOutCleanup = () => void | Promise<void>;

export type IdentityListener = (userId: string | null) => void;

const cleanups = new Set<SignOutCleanup>();
let signedIn = false;

export function onSignOut(cleanup: SignOutCleanup): () => void {
  cleanups.add(cleanup);
  return () => {
    cleanups.delete(cleanup);
  };
}

const identityListeners = new Set<IdentityListener>();

export function onIdentityChange(listener: IdentityListener): () => void {
  identityListeners.add(listener);
  return () => {
    identityListeners.delete(listener);
  };
}

export function notifyIdentityChange(userId: string | null): void {
  for (const listener of identityListeners) listener(userId);
  setSignedInUser(userId !== null);
}

export function runSignOutCleanups(): void {
  bumpSessionEpoch();
  for (const cleanup of cleanups) {
    try {
      void Promise.resolve(cleanup()).catch(warnCleanupFailed);
    } catch (error) {
      warnCleanupFailed(error);
    }
  }
}

const MAX_MESSAGE_LENGTH = 120;

function warnCleanupFailed(error: unknown): void {
  const name = error instanceof Error ? error.name : 'non-Error';
  const message = error instanceof Error ? error.message.slice(0, MAX_MESSAGE_LENGTH) : '';
  console.warn(`[session] sign-out cleanup failed: ${name} ${message}`.trimEnd());
}

export function hasSignedInUser(): boolean {
  return signedIn;
}

export function setSignedInUser(isSignedIn: boolean): void {
  signedIn = isSignedIn;
}
