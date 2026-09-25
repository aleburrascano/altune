// Client-side defence-in-depth against hammering one account's sign-in or
// password-reset form (#1640). Supabase throttles server-side; this refuses
// locally, so a scripted — or merely frantic — retry loop never leaves the
// device in the first place.
//
// Nothing here is derived from what the server said, only from how many times
// this device submitted without succeeding. A locked-out address and one that
// was never tried are therefore indistinguishable to a stranger, so the lockout
// adds no enumeration oracle of its own.
//
// The runs are module-scoped rather than per-hook on purpose: the sign-in screen
// unmounts when the user steps over to "forgot password" and back, and a lockout
// a remount clears would stop nothing. They are memory-only — never persisted,
// never logged, both because the keys are addresses and because an app restart
// resetting them is accepted: this is depth behind the backend's limit, not the
// limit itself.

/** Failures in one run before the account is refused locally. */
export const LOCKOUT_AFTER_FAILURES = 5;

/** Cooldown earned by the first refusal; each further failure doubles it. */
export const LOCKOUT_BASE_MS = 15_000;

/** Caps a single cooldown, and how long a quiet run is remembered at all. */
export const FAILURE_RUN_MEMORY_MS = 5 * 60_000;

/** So a driver cycling addresses cannot grow the map without end. */
export const MAX_TRACKED_ACCOUNTS = 64;

export type LockoutAction = 'sign-in' | 'reset-request';

type FailureRun = { count: number; lastFailureAt: number };

const runsByAccount = new Map<string, FailureRun>();

// Case and surrounding space must not mint a fresh allowance: ` A@B.co ` is the
// same account as `a@b.co`. `toLowerCase`, never `toLocaleLowerCase`, so a
// Turkish device folds `I` the way every other device does.
function accountKey(action: LockoutAction, email: string): string {
  return `${action}:${email.trim().toLowerCase()}`;
}

function cooldownMs(failures: number): number {
  if (failures < LOCKOUT_AFTER_FAILURES) return 0;
  const doubledPerExtraFailure = LOCKOUT_BASE_MS * 2 ** (failures - LOCKOUT_AFTER_FAILURES);
  return Math.min(doubledPerExtraFailure, FAILURE_RUN_MEMORY_MS);
}

function runFor(action: LockoutAction, email: string, now: number): FailureRun | undefined {
  const run = runsByAccount.get(accountKey(action, email));
  if (!run) return undefined;
  return now < run.lastFailureAt + FAILURE_RUN_MEMORY_MS ? run : undefined;
}

function forgetStaleRuns(now: number): void {
  for (const [key, run] of runsByAccount) {
    if (now >= run.lastFailureAt + FAILURE_RUN_MEMORY_MS) runsByAccount.delete(key);
  }
}

function forgetLeastRecentRun(): void {
  let leastRecentKey: string | undefined;
  let leastRecentAt = Infinity;
  for (const [key, run] of runsByAccount) {
    if (run.lastFailureAt >= leastRecentAt) continue;
    leastRecentAt = run.lastFailureAt;
    leastRecentKey = key;
  }
  if (leastRecentKey !== undefined) runsByAccount.delete(leastRecentKey);
}

export function isLockedOut(
  action: LockoutAction,
  email: string,
  now: number = Date.now(),
): boolean {
  const run = runFor(action, email, now);
  if (!run) return false;
  return now < run.lastFailureAt + cooldownMs(run.count);
}

function prepareToStore(key: string, now: number): void {
  forgetStaleRuns(now);
  if (!runsByAccount.has(key) && runsByAccount.size >= MAX_TRACKED_ACCOUNTS) {
    forgetLeastRecentRun();
  }
}

export function recordFailedAttempt(
  action: LockoutAction,
  email: string,
  now: number = Date.now(),
): void {
  const key = accountKey(action, email);
  const count = (runFor(action, email, now)?.count ?? 0) + 1;
  prepareToStore(key, now);
  runsByAccount.set(key, { count, lastFailureAt: now });
}

export function clearFailedAttempts(action: LockoutAction, email: string): void {
  runsByAccount.delete(accountKey(action, email));
}

type LockedOut = { kind: 'error'; reason: 'too_many_attempts' };

const LOCKED_OUT: LockedOut = { kind: 'error', reason: 'too_many_attempts' };

/**
 * Wraps an auth SDK call so that the account named by its **first argument**
 * earns a cooldown after repeated failures.
 *
 * Any terminal `error` counts as a failure whatever its reason — what the server
 * answered must not change how soon the client will ask again — and any other
 * terminal state ends the run.
 */
export function lockoutOnRepeatedFailure<
  A extends [string, ...unknown[]],
  R extends { kind: string },
>(
  action: LockoutAction,
  attempt: (...args: A) => Promise<R>,
): (...args: A) => Promise<R | LockedOut> {
  return (...args: A) => guardedCall(action, attempt, args);
}

async function guardedCall<A extends [string, ...unknown[]], R extends { kind: string }>(
  action: LockoutAction,
  attempt: (...args: A) => Promise<R>,
  args: A,
): Promise<R | LockedOut> {
  const [email] = args;
  if (isLockedOut(action, email)) return LOCKED_OUT;
  return settleRun(action, email, await attempt(...args));
}

function settleRun<R extends { kind: string }>(
  action: LockoutAction,
  email: string,
  outcome: R,
): R {
  if (outcome.kind === 'error') recordFailedAttempt(action, email);
  else clearFailedAttempts(action, email);
  return outcome;
}

export function _resetLockoutsForTest(): void {
  runsByAccount.clear();
}
