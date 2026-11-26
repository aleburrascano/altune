export type LockoutAction = 'sign-in' | 'reset-request';

export type LockedOut = { kind: 'error'; reason: 'too_many_attempts' };

export interface AttemptLockout {
  guard<A extends [string, ...unknown[]], R extends { kind: string }>(
    action: LockoutAction,
    attempt: (...args: A) => Promise<R>,
    countsAsFailure: (outcome: R) => boolean,
  ): (...args: A) => Promise<R | LockedOut>;
}
