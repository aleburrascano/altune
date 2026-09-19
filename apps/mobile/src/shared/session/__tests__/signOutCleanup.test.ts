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
  /** Stands in for the object react-query builds per run and hands to every attempt. */
  const runContext = () => ({ client: new QueryClient(), meta: undefined });

  it('settles a mutation of the current session, with the context its onMutate returned', async () => {
    const cache: string[] = [];
    const options = guardedMutationOptions({
      mutationFn: () => Promise.reject(new Error('clear failed')),
      onMutate: () => ({ previous: 'the history before the clear' }),
      onError: (_error, _variables, context) => cache.push(context.previous),
    });

    const context = await options.onMutate!(undefined, runContext());
    options.onError!(new Error('clear failed'), undefined, context, runContext());

    expect(cache).toEqual(['the history before the clear']);
  });

  it('sends an attempt of the session the mutation started in', async () => {
    const send = jest.fn(() => Promise.resolve('cleared'));
    const options = guardedMutationOptions({ mutationFn: send });
    const run = runContext();
    await options.onMutate!(undefined, run);

    await expect(options.mutationFn!(undefined, run)).resolves.toBe('cleared');
  });

  // #1752: react-query re-invokes mutationFn on every retry, and each attempt
  // re-derives its bearer token, so one firing after the switch would act as B.
  it('refuses a reattempt of a mutation whose session has since ended', async () => {
    const send = jest.fn(() => Promise.resolve('cleared'));
    const options = guardedMutationOptions({ mutationFn: send });
    const run = runContext();
    await options.onMutate!(undefined, run);
    await options.mutationFn!(undefined, run);

    runSignOutCleanups();

    await expect(options.mutationFn!(undefined, run)).rejects.toThrow(/session .* has ended/);
    expect(send).toHaveBeenCalledTimes(1);
  });

  it('refuses an attempt of a run that never captured a session', async () => {
    const send = jest.fn(() => Promise.resolve('cleared'));
    const options = guardedMutationOptions({ mutationFn: send });

    await expect(options.mutationFn!(undefined, runContext())).rejects.toThrow(
      /session .* has ended/,
    );
    expect(send).not.toHaveBeenCalled();
  });

  it('skips the settle callback of a mutation whose session has since ended', async () => {
    const cache: string[] = [];
    const options = guardedMutationOptions({
      mutationFn: () => Promise.resolve('cleared'),
      onSuccess: (data) => cache.push(data),
    });
    const context = await options.onMutate!(undefined, runContext());

    runSignOutCleanups();
    options.onSuccess!('cleared', undefined, context, runContext());

    expect(cache).toEqual([]);
  });
});
