import { monotonicNow } from '@shared/timing';
import type { AttemptLockout, LockedOut, LockoutAction } from '@features/signIn/ports/ports';

const LOCKOUT_AFTER_FAILURES = 5;

const LOCKOUT_BASE_MS = 15_000;

const FAILURE_RUN_MEMORY_MS = 300_000;

const MAX_TRACKED_ACCOUNTS = 64;

type FailureRun = { count: number; lastFailureAt: number; lastFailureTick: number };

type FailureRuns = Map<string, FailureRun>;

export interface AttemptLockoutStore extends AttemptLockout {
  isLockedOut(action: LockoutAction, email: string, now?: number, tick?: number): boolean;
  recordFailedAttempt(action: LockoutAction, email: string, now?: number, tick?: number): void;
  clearFailedAttempts(action: LockoutAction, email: string): void;
}

const LOCKED_OUT: LockedOut = { kind: 'error', reason: 'too_many_attempts' };

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
  runs: FailureRuns,
  action: LockoutAction,
  email: string,
  now: number,
  tick: number,
): FailureRun | undefined {
  const run = runs.get(accountKey(action, email));
  if (!run) return undefined;
  return msSinceFailure(run, now, tick) < FAILURE_RUN_MEMORY_MS ? run : undefined;
}

function forgetStaleRuns(runs: FailureRuns, now: number, tick: number): void {
  for (const [key, run] of runs) {
    if (msSinceFailure(run, now, tick) >= FAILURE_RUN_MEMORY_MS) runs.delete(key);
  }
}

function forgetLeastRecentRun(runs: FailureRuns): void {
  let leastRecentKey: string | undefined;
  let leastRecentAt = Infinity;
  for (const [key, run] of runs) {
    if (run.lastFailureAt >= leastRecentAt) continue;
    leastRecentAt = run.lastFailureAt;
    leastRecentKey = key;
  }
  if (leastRecentKey !== undefined) runs.delete(leastRecentKey);
}

function prepareToStore(runs: FailureRuns, key: string, now: number, tick: number): void {
  forgetStaleRuns(runs, now, tick);
  if (!runs.has(key) && runs.size >= MAX_TRACKED_ACCOUNTS) {
    forgetLeastRecentRun(runs);
  }
}

export function createAttemptLockoutStore(): AttemptLockoutStore {
  const runs: FailureRuns = new Map();

  function isLockedOut(
    action: LockoutAction,
    email: string,
    now: number = Date.now(),
    tick: number = monotonicNow(),
  ): boolean {
    const run = runFor(runs, action, email, now, tick);
    if (!run) return false;
    return msSinceFailure(run, now, tick) < cooldownMs(run.count);
  }

  function recordFailedAttempt(
    action: LockoutAction,
    email: string,
    now: number = Date.now(),
    tick: number = monotonicNow(),
  ): void {
    const key = accountKey(action, email);
    const count = (runFor(runs, action, email, now, tick)?.count ?? 0) + 1;
    prepareToStore(runs, key, now, tick);
    runs.set(key, { count, lastFailureAt: now, lastFailureTick: tick });
  }

  function clearFailedAttempts(action: LockoutAction, email: string): void {
    runs.delete(accountKey(action, email));
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

  function guard<A extends [string, ...unknown[]], R extends { kind: string }>(
    action: LockoutAction,
    attempt: (...args: A) => Promise<R>,
    countsAsFailure: (outcome: R) => boolean,
  ): (...args: A) => Promise<R | LockedOut> {
    return (...args: A) => guardedCall(action, attempt, countsAsFailure, args);
  }

  return { guard, isLockedOut, recordFailedAttempt, clearFailedAttempts };
}
