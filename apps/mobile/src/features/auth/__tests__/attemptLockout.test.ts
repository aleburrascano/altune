import {
  FAILURE_RUN_MEMORY_MS,
  LOCKOUT_AFTER_FAILURES,
  LOCKOUT_BASE_MS,
  MAX_TRACKED_ACCOUNTS,
  clearFailedAttempts,
  isLockedOut,
  lockoutOnRepeatedFailure,
  recordFailedAttempt,
  _resetLockoutsForTest,
  type LockoutAction,
} from '../attemptLockout';

const START = 1_000_000;

function failTimes(email: string, times: number, at: number = START): void {
  for (let i = 0; i < times; i += 1) recordFailedAttempt('sign-in', email, at);
}

beforeEach(() => {
  _resetLockoutsForTest();
});

describe('attemptLockout: the cooldown a run of failures earns', () => {
  it('leaves an account that has never been tried open', () => {
    expect(isLockedOut('sign-in', 'a@b.co', START)).toBe(false);
  });

  it('stays open for the failure before the threshold', () => {
    failTimes('a@b.co', LOCKOUT_AFTER_FAILURES - 1);

    expect(isLockedOut('sign-in', 'a@b.co', START)).toBe(false);
  });

  it('closes on the threshold failure', () => {
    failTimes('a@b.co', LOCKOUT_AFTER_FAILURES);

    expect(isLockedOut('sign-in', 'a@b.co', START)).toBe(true);
    expect(isLockedOut('sign-in', 'a@b.co', START + LOCKOUT_BASE_MS - 1)).toBe(true);
  });

  it('reopens once the cooldown has elapsed', () => {
    failTimes('a@b.co', LOCKOUT_AFTER_FAILURES);

    expect(isLockedOut('sign-in', 'a@b.co', START + LOCKOUT_BASE_MS)).toBe(false);
  });

  it('doubles the cooldown for a failure that follows the lockout', () => {
    failTimes('a@b.co', LOCKOUT_AFTER_FAILURES);
    const afterCooldown = START + LOCKOUT_BASE_MS;
    recordFailedAttempt('sign-in', 'a@b.co', afterCooldown);

    expect(isLockedOut('sign-in', 'a@b.co', afterCooldown + 2 * LOCKOUT_BASE_MS - 1)).toBe(true);
    expect(isLockedOut('sign-in', 'a@b.co', afterCooldown + 2 * LOCKOUT_BASE_MS)).toBe(false);
  });

  it('caps the doubling at the memory window, so a run can never lock an account for good', () => {
    failTimes('a@b.co', LOCKOUT_AFTER_FAILURES + 20);

    expect(isLockedOut('sign-in', 'a@b.co', START + FAILURE_RUN_MEMORY_MS - 1)).toBe(true);
    expect(isLockedOut('sign-in', 'a@b.co', START + FAILURE_RUN_MEMORY_MS)).toBe(false);
  });

  it('forgets a run that has gone quiet, rather than resuming its escalation', () => {
    failTimes('a@b.co', LOCKOUT_AFTER_FAILURES);
    const afterMemory = START + FAILURE_RUN_MEMORY_MS;
    failTimes('a@b.co', LOCKOUT_AFTER_FAILURES - 1, afterMemory);

    expect(isLockedOut('sign-in', 'a@b.co', afterMemory)).toBe(false);
  });

  it('counts the same address written in another case or with spaces as one account', () => {
    failTimes(' A@B.co ', LOCKOUT_AFTER_FAILURES - 1);
    recordFailedAttempt('sign-in', 'a@b.co', START);

    expect(isLockedOut('sign-in', 'A@B.CO', START)).toBe(true);
  });

  it('locks the account that failed and no other', () => {
    failTimes('a@b.co', LOCKOUT_AFTER_FAILURES);

    expect(isLockedOut('sign-in', 'other@b.co', START)).toBe(false);
  });

  it('reopens the account as soon as its run is cleared', () => {
    failTimes('a@b.co', LOCKOUT_AFTER_FAILURES);
    clearFailedAttempts('sign-in', 'A@B.co');

    expect(isLockedOut('sign-in', 'a@b.co', START)).toBe(false);
  });

  it('keeps the newest runs when more accounts fail than it will track', () => {
    failTimes('first@b.co', LOCKOUT_AFTER_FAILURES);
    for (let i = 0; i < MAX_TRACKED_ACCOUNTS; i += 1) failTimes(`filler${i}@b.co`, 1, START + 1);
    failTimes('last@b.co', LOCKOUT_AFTER_FAILURES, START + 2);

    expect(isLockedOut('sign-in', 'last@b.co', START + 2)).toBe(true);
    expect(isLockedOut('sign-in', 'first@b.co', START + 2)).toBe(false);
  });
});

describe('attemptLockout: wrapping an auth call', () => {
  const invalidCredentials = { kind: 'error', reason: 'invalid_credentials' } as const;

  it('refuses without calling through once the account is locked out', async () => {
    const attempt = jest.fn().mockResolvedValue(invalidCredentials);
    const guarded = lockoutOnRepeatedFailure('sign-in', attempt);

    for (let i = 0; i < LOCKOUT_AFTER_FAILURES; i += 1) await guarded('a@b.co');

    expect(await guarded('a@b.co')).toEqual({ kind: 'error', reason: 'too_many_attempts' });
    expect(attempt).toHaveBeenCalledTimes(LOCKOUT_AFTER_FAILURES);
  });

  it('ends the run on a terminal state that is not an error', async () => {
    const attempt = jest.fn().mockResolvedValue(invalidCredentials);
    const guarded = lockoutOnRepeatedFailure('sign-in', attempt);
    for (let i = 0; i < LOCKOUT_AFTER_FAILURES - 1; i += 1) await guarded('a@b.co');
    attempt.mockResolvedValue({ kind: 'ok' });
    await guarded('a@b.co');

    attempt.mockResolvedValue(invalidCredentials);
    for (let i = 0; i < LOCKOUT_AFTER_FAILURES - 1; i += 1) await guarded('a@b.co');

    expect(isLockedOut('sign-in', 'a@b.co')).toBe(false);
  });

  it('passes every argument through to the wrapped call', async () => {
    const attempt = jest.fn().mockResolvedValue({ kind: 'ok' });

    await lockoutOnRepeatedFailure('sign-in', attempt)('a@b.co', 'hunter2');

    expect(attempt).toHaveBeenCalledWith('a@b.co', 'hunter2');
  });
});

function failActionTimes(action: LockoutAction, times: number): void {
  for (let i = 0; i < times; i += 1) recordFailedAttempt(action, 'a@b.co', START);
}

describe('attemptLockout: sign-in and reset-request keep separate runs', () => {
  it('does not let a successful reset request zero the sign-in run', () => {
    failActionTimes('sign-in', LOCKOUT_AFTER_FAILURES - 1);
    clearFailedAttempts('reset-request', 'a@b.co');
    recordFailedAttempt('sign-in', 'a@b.co', START);

    expect(isLockedOut('sign-in', 'a@b.co', START)).toBe(true);
  });

  it('does not block a reset request after the sign-in threshold', () => {
    failActionTimes('sign-in', LOCKOUT_AFTER_FAILURES);

    expect(isLockedOut('sign-in', 'a@b.co', START)).toBe(true);
    expect(isLockedOut('reset-request', 'a@b.co', START)).toBe(false);
  });
});
