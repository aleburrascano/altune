export const LOCKOUT_AFTER_FAILURES = 5;

export const LOCKOUT_BASE_MS = 15_000;

export const FAILURE_RUN_MEMORY_MS = 5 * 60_000;

export const MAX_TRACKED_ACCOUNTS = 64;

export type LockoutAction = 'sign-in' | 'reset-request';

type FailureRun = { count: number; lastFailureAt: number };

const runsByAccount = new Map<string, FailureRun>();

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
