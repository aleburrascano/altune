import { QueryClient } from '@tanstack/react-query';

import type * as signOutCleanup from '@shared/session/signOutCleanup';

import {
  currentSessionEpoch,
  hasSignedInUser,
  isSameSession,
  onSignOut,
  runSignOutCleanups,
  notifyIdentityChange,
  onIdentityChange,
} from '@shared/session/signOutCleanup';
import { guardedMutationOptions, SessionEndedError } from '@shared/session/guardedMutation';

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

  it('warns once per throwing or rejecting cleanup, and still runs the healthy one', async () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    const healthy = jest.fn();
    const offs = [
      onSignOut(() => {
        throw new Error('sync boom');
      }),
      onSignOut(() => Promise.reject(new TypeError('async boom'))),
      onSignOut(healthy),
    ];

    runSignOutCleanups();
    await Promise.resolve();
    await Promise.resolve();

    const lines = warn.mock.calls.map((call) => String(call[0]));
    expect(lines).toHaveLength(2);
    expect(lines.join('\n')).toMatch(/Error sync boom/);
    expect(lines.join('\n')).toMatch(/TypeError async boom/);
    expect(healthy).toHaveBeenCalledTimes(1);
    offs.forEach((off) => off());
    warn.mockRestore();
  });

  it('reports whether a user is signed in, defaulting to no one', () => {
    expect(hasSignedInUser()).toBe(false);
    notifyIdentityChange('user-1');
    expect(hasSignedInUser()).toBe(true);
    notifyIdentityChange(null);
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

  it('rejects a fenced reattempt as the exported SessionEndedError', async () => {
    const options = guardedMutationOptions({ mutationFn: () => Promise.resolve('cleared') });
    const run = runContext();
    await options.onMutate!(undefined, run);
    runSignOutCleanups();

    await expect(options.mutationFn!(undefined, run)).rejects.toBeInstanceOf(SessionEndedError);
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

describe('identity change', () => {
  describe('identity change registry', () => {
    it('runs listeners in registration order, then sets the signed-in flag', () => {
      const calls: string[] = [];
      const stopFirst = onIdentityChange(() => calls.push(`first:${String(hasSignedInUser())}`));
      const stopSecond = onIdentityChange((userId) => calls.push(`second:${String(userId)}`));
      notifyIdentityChange('user-1');
      expect(calls).toEqual(['first:false', 'second:user-1']);
      expect(hasSignedInUser()).toBe(true);
      stopFirst();
      stopSecond();
      notifyIdentityChange(null);
      expect(calls).toHaveLength(2);
      expect(hasSignedInUser()).toBe(false);
    });
  });
});

describe('primeSignedInState', () => {
  function freshRegistry(): Registry {
    let registry!: Registry;
    jest.isolateModules(() => {
      registry = jest.requireActual('@shared/session/signOutCleanup');
    });
    return registry;
  }

  type Registry = typeof signOutCleanup;

  it('reads as signed-in when primed signed-in while unknown', () => {
    const registry = freshRegistry();
    expect(registry.hasSignedInUser()).toBe(false);

    registry.primeSignedInState(true);

    expect(registry.hasSignedInUser()).toBe(true);
  });

  it('reads as signed-out when primed signed-out while unknown', () => {
    const registry = freshRegistry();

    registry.primeSignedInState(false);

    expect(registry.hasSignedInUser()).toBe(false);
  });

  it('keeps a real signed-out identity change over a later signed-in prime', () => {
    const registry = freshRegistry();
    registry.notifyIdentityChange(null);

    registry.primeSignedInState(true);

    expect(registry.hasSignedInUser()).toBe(false);
  });

  it('keeps a real signed-in identity change over a later signed-out prime', () => {
    const registry = freshRegistry();
    registry.notifyIdentityChange('user-1');

    registry.primeSignedInState(false);

    expect(registry.hasSignedInUser()).toBe(true);
  });
});

describe('primeSignedInState, after the first answer', () => {
  function freshRegistry(): typeof signOutCleanup {
    let registry!: typeof signOutCleanup;
    jest.isolateModules(() => {
      registry = jest.requireActual('@shared/session/signOutCleanup');
    });
    return registry;
  }

  it('ignores a second prime once a first prime has answered signed-in', () => {
    const registry = freshRegistry();
    registry.primeSignedInState(true);

    registry.primeSignedInState(false);

    expect(registry.hasSignedInUser()).toBe(true);
  });

  it('ignores a second prime once a first prime has answered signed-out', () => {
    const registry = freshRegistry();
    registry.primeSignedInState(false);

    registry.primeSignedInState(true);

    expect(registry.hasSignedInUser()).toBe(false);
  });

  it('reads as signed-out when the user signs out after a signed-in prime', () => {
    const registry = freshRegistry();
    registry.primeSignedInState(true);

    registry.notifyIdentityChange(null);

    expect(registry.hasSignedInUser()).toBe(false);
  });

  it('reads as signed-in when the user signs in after a signed-out prime', () => {
    const registry = freshRegistry();
    registry.primeSignedInState(false);

    registry.notifyIdentityChange('user-1');

    expect(registry.hasSignedInUser()).toBe(true);
  });

  it('does not read as signed-in when primed signed-in after a sign-out cleanup ran', () => {
    const registry = freshRegistry();
    registry.notifyIdentityChange('user-1');
    registry.runSignOutCleanups();
    registry.notifyIdentityChange(null);

    registry.primeSignedInState(true);

    expect(registry.hasSignedInUser()).toBe(false);
  });
});
