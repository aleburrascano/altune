import { QueryClient } from '@tanstack/react-query';

import {
  currentSessionEpoch,
  guardedMutationOptions,
  hasSignedInUser,
  isSameSession,
  onSignOut,
  runSignOutCleanups,
  setSignedInUser,
} from '../signOutCleanup';

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

  it('marks an epoch captured before an identity change as a different session', () => {
    const captured = currentSessionEpoch();
    expect(isSameSession(captured)).toBe(true);

    runSignOutCleanups();

    expect(isSameSession(captured)).toBe(false);
    expect(isSameSession(currentSessionEpoch())).toBe(true);
    expect(isSameSession(undefined)).toBe(false);
  });
});

describe('guardedMutationOptions', () => {
  /** react-query hands every callback this; the fence ignores it. */
  const callbackContext = () => ({ client: new QueryClient(), meta: undefined });

  it('settles a mutation of the current session, with the context its onMutate returned', async () => {
    const cache: string[] = [];
    const options = guardedMutationOptions({
      mutationFn: () => Promise.reject(new Error('clear failed')),
      onMutate: () => ({ previous: 'the history before the clear' }),
      onError: (_error, _variables, context) => cache.push(context.previous),
    });

    const context = await options.onMutate!(undefined, callbackContext());
    options.onError!(new Error('clear failed'), undefined, context, callbackContext());

    expect(cache).toEqual(['the history before the clear']);
  });

  it('skips the settle callback of a mutation whose session has since ended', async () => {
    const cache: string[] = [];
    const options = guardedMutationOptions({
      mutationFn: () => Promise.resolve('cleared'),
      onSuccess: (data) => cache.push(data),
    });
    const context = await options.onMutate!(undefined, callbackContext());

    runSignOutCleanups();
    options.onSuccess!('cleared', undefined, context, callbackContext());

    expect(cache).toEqual([]);
  });
});
