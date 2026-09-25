import {
  LOCKOUT_AFTER_FAILURES,
  clearFailedAttempts,
  isLockedOut,
  recordFailedAttempt,
  _resetLockoutsForTest,
  type LockoutAction,
} from '../attemptLockout';

const START = 1_000_000;

function failTimes(action: LockoutAction, times: number): void {
  for (let i = 0; i < times; i += 1) recordFailedAttempt(action, 'a@b.co', START);
}

beforeEach(() => {
  _resetLockoutsForTest();
});

describe('attemptLockout: sign-in and reset-request keep separate runs', () => {
  it('does not let a successful reset request zero the sign-in run', () => {
    failTimes('sign-in', LOCKOUT_AFTER_FAILURES - 1);
    clearFailedAttempts('reset-request', 'a@b.co');
    recordFailedAttempt('sign-in', 'a@b.co', START);

    expect(isLockedOut('sign-in', 'a@b.co', START)).toBe(true);
  });

  it('does not block a reset request after the sign-in threshold', () => {
    failTimes('sign-in', LOCKOUT_AFTER_FAILURES);

    expect(isLockedOut('sign-in', 'a@b.co', START)).toBe(true);
    expect(isLockedOut('reset-request', 'a@b.co', START)).toBe(false);
  });
});
