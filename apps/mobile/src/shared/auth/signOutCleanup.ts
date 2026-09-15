// Sign-out cleanup registry. Module-level state (zustand stores, native players,
// on-disk caches) outlives the React tree AuthGate unmounts on sign-out, so any
// slice that holds one user's data outside react-query registers a cleanup here.
// useSession runs every cleanup whenever the signed-in identity changes (sign-out,
// or a direct switch to another account) and tracks whether anyone is signed in,
// for code that runs outside React (e.g. the headless playback service).
//
// Register once at module load or app boot; a cleanup must be idempotent (several
// useSession instances may observe the same change) and must not throw — one that
// does is isolated so the remaining cleanups still run.

export type SignOutCleanup = () => void | Promise<void>;

const cleanups = new Set<SignOutCleanup>();
let signedIn = false;
let sessionEpoch = 0;

/** Registers `cleanup` to run on every identity change; returns an unregister function. */
export function onSignOut(cleanup: SignOutCleanup): () => void {
  cleanups.add(cleanup);
  return () => {
    cleanups.delete(cleanup);
  };
}

export function runSignOutCleanups(): void {
  sessionEpoch += 1;
  for (const cleanup of cleanups) {
    try {
      void Promise.resolve(cleanup()).catch(() => undefined);
    } catch {
      // Isolated: a failing cleanup must not keep the next user's data from being cleared.
    }
  }
}

/** Whether a user is currently signed in, as last observed by useSession. */
export function hasSignedInUser(): boolean {
  return signedIn;
}

export function setSignedInUser(isSignedIn: boolean): void {
  signedIn = isSignedIn;
}

/**
 * Advances on every identity change (each runSignOutCleanups). A mutation captures
 * it when it starts; if it no longer matches on settle, the user who started the
 * mutation is gone and its late callback must not touch the shared query cache.
 */
export function currentSessionEpoch(): number {
  return sessionEpoch;
}

export function isSameSession(epoch: number | undefined): boolean {
  return epoch === sessionEpoch;
}
