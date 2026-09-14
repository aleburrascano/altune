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

/** Registers `cleanup` to run on every identity change; returns an unregister function. */
export function onSignOut(cleanup: SignOutCleanup): () => void {
  cleanups.add(cleanup);
  return () => {
    cleanups.delete(cleanup);
  };
}

export function runSignOutCleanups(): void {
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
