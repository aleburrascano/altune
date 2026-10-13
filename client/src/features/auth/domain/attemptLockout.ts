import { monotonicNow } from '@shared/time';

export const LOCKOUT_AFTER_FAILURES = 5;

export const LOCKOUT_BASE_MS = 15_000;

export const FAILURE_RUN_MEMORY_MS = 5 * 60_000;

export const MAX_TRACKED_ACCOUNTS = 64;

export type LockoutAction = 'sign-in' | 'reset-request';

type FailureRun = { count: number; lastFailureAt: number; lastFailureTick: number };

const runsByAccount = new Map<string, FailureRun>();

function accountKey(action: LockoutAction, email: string): string {
  return `${action}:${email.trim().toLowerCase()}`;
}

function cooldownMs(failures: number): number {
  if (failures < LOCKOUT_AFTER_FAILURES) return 0;
  const doubledPerExtraFailure = LOCKOUT_BASE_MS * 2 ** (failures - LOCKOUT_AFTER_FAILURES);
  return Math.min(doubledPerExtraFailure, FAILURE_RUN_MEMORY_MS);
}

function msSinceFailure(run: FailureRun, now: number, tick: number): number {
  if (now < run.lastFailureAt) return Infinity;
  return Math.max(now - run.lastFailureAt, tick - run.lastFailureTick);
}

function runFor(
  action: LockoutAction,
  email: string,
  now: number,
  tick: number,
): FailureRun | undefined {
  const run = runsByAccount.get(accountKey(action, email));
  if (!run) return undefined;
  return msSinceFailure(run, now, tick) < FAILURE_RUN_MEMORY_MS ? run : undefined;
}

function forgetStaleRuns(now: number, tick: number): void {
  for (const [key, run] of runsByAccount) {
    if (msSinceFailure(run, now, tick) >= FAILURE_RUN_MEMORY_MS) runsByAccount.delete(key);
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
  tick: number = monotonicNow(),
): boolean {
  const run = runFor(action, email, now, tick);
  if (!run) return false;
  return msSinceFailure(run, now, tick) < cooldownMs(run.count);
}

function prepareToStore(key: string, now: number, tick: number): void {
  forgetStaleRuns(now, tick);
  if (!runsByAccount.has(key) && runsByAccount.size >= MAX_TRACKED_ACCOUNTS) {
    forgetLeastRecentRun();
  }
}

export function recordFailedAttempt(
  action: LockoutAction,
  email: string,
  now: number = Date.now(),
  tick: number = monotonicNow(),
): void {
  const key = accountKey(action, email);
  const count = (runFor(action, email, now, tick)?.count ?? 0) + 1;
  prepareToStore(key, now, tick);
  runsByAccount.set(key, { count, lastFailureAt: now, lastFailureTick: tick });
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
  countsAsFailure: (outcome: R) => boolean,
): (...args: A) => Promise<R | LockedOut> {
  return (...args: A) => guardedCall(action, attempt, countsAsFailure, args);
}

async function guardedCall<A extends [string, ...unknown[]], R extends { kind: string }>(
  action: LockoutAction,
  attempt: (...args: A) => Promise<R>,
  countsAsFailure: (outcome: R) => boolean,
  args: A,
): Promise<R | LockedOut> {
  const [email] = args;
  if (isLockedOut(action, email)) return LOCKED_OUT;
  return settleRun(action, email, await attempt(...args), countsAsFailure);
}

function settleRun<R extends { kind: string }>(
  action: LockoutAction,
  email: string,
  outcome: R,
  countsAsFailure: (outcome: R) => boolean,
): R {
  if (outcome.kind !== 'error') clearFailedAttempts(action, email);
  else if (countsAsFailure(outcome)) recordFailedAttempt(action, email);
  return outcome;
}

export function _resetLockoutsForTest(): void {
  runsByAccount.clear();
}
