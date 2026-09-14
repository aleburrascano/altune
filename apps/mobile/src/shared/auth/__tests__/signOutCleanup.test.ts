import { hasSignedInUser, onSignOut, runSignOutCleanups, setSignedInUser } from '../signOutCleanup';

describe('signOutCleanup registry', () => {
  it('runs every registered cleanup, once per registration', () => {
    const first = jest.fn();
    const second = jest.fn();
    const offFirst = onSignOut(first);
    const offSecond = onSignOut(second);
    onSignOut(first);

    runSignOutCleanups();

    expect(first).toHaveBeenCalledTimes(1);
    expect(second).toHaveBeenCalledTimes(1);
    offFirst();
    offSecond();
  });

  it('stops running a cleanup once it is unregistered', () => {
    const cleanup = jest.fn();
    const off = onSignOut(cleanup);
    off();

    runSignOutCleanups();

    expect(cleanup).not.toHaveBeenCalled();
  });

  it('isolates a throwing or rejecting cleanup so the rest still run', async () => {
    const after = jest.fn();
    const offs = [
      onSignOut(() => {
        throw new Error('sync boom');
      }),
      onSignOut(() => Promise.reject(new Error('async boom'))),
      onSignOut(after),
    ];

    expect(() => runSignOutCleanups()).not.toThrow();
    await Promise.resolve();

    expect(after).toHaveBeenCalledTimes(1);
    offs.forEach((off) => off());
  });

  it('reports whether a user is signed in, defaulting to no one', () => {
    expect(hasSignedInUser()).toBe(false);
    setSignedInUser(true);
    expect(hasSignedInUser()).toBe(true);
    setSignedInUser(false);
    expect(hasSignedInUser()).toBe(false);
  });
});
