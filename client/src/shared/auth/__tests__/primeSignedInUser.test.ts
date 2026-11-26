import type * as signOutCleanup from '@shared/session';
import type * as authDoor from '@shared/auth';
import { authClientOver } from '@testing/fakeAuthClient';

type Modules = {
  registry: typeof signOutCleanup;
  primeSignedInUser: typeof authDoor.primeSignedInUser;
  warn: jest.Mock;
};

function freshModules(getSession: () => Promise<unknown>): Modules {
  let modules!: Modules;
  jest.isolateModules(() => {
    const warn = jest.fn();
    jest.requireActual('@shared/log').setLogger({ warn });
    const auth: typeof authDoor = jest.requireActual('@shared/auth');
    auth.installAuthClient(authClientOver({ getSession }));
    modules = {
      registry: jest.requireActual('@shared/session'),
      primeSignedInUser: auth.primeSignedInUser,
      warn,
    };
  });
  return modules;
}

function sessionOf(hasSession: boolean) {
  return Promise.resolve({
    data: { session: hasSession ? { user: { id: 'user-1' } } : null },
    error: null,
  });
}

describe('primeSignedInUser', () => {
  it('reads as signed-in after a persisted session is found without any identity change', async () => {
    const { registry, primeSignedInUser } = freshModules(() => sessionOf(true));
    expect(registry.hasSignedInUser()).toBe(false);

    await primeSignedInUser();

    expect(registry.hasSignedInUser()).toBe(true);
  });

  it('reads as signed-out when no session is persisted', async () => {
    const { registry, primeSignedInUser } = freshModules(() => sessionOf(false));

    await primeSignedInUser();

    expect(registry.hasSignedInUser()).toBe(false);
  });

  it('does not override an identity change that lands while the session read is pending', async () => {
    let resolveSession!: (value: unknown) => void;
    const { registry, primeSignedInUser } = freshModules(
      () => new Promise((resolve) => (resolveSession = resolve)),
    );
    const priming = primeSignedInUser();

    registry.notifyIdentityChange(null);
    resolveSession(await sessionOf(true));
    await priming;

    expect(registry.hasSignedInUser()).toBe(false);
  });

  it('stays unknown, and later real identity still applies, when the session read fails', async () => {
    const { registry, primeSignedInUser } = freshModules(() =>
      Promise.reject(new Error('storage down')),
    );

    await expect(primeSignedInUser()).resolves.toBeUndefined();
    expect(registry.hasSignedInUser()).toBe(false);
    registry.primeSignedInState(true);

    expect(registry.hasSignedInUser()).toBe(true);
  });
});

describe('primeSignedInUser, failure and race paths', () => {
  it('warns when the stored session cannot be read', async () => {
    const { primeSignedInUser, warn } = freshModules(() =>
      Promise.reject(new Error('storage down')),
    );

    await primeSignedInUser();

    expect(warn).toHaveBeenCalledTimes(1);
    expect(warn).toHaveBeenCalledWith('[auth] headless sign-in prime failed', expect.any(Error));
  });

  it('resolves and stays unknown when the session read throws before returning a promise', async () => {
    const { registry, primeSignedInUser } = freshModules(() => {
      throw new Error('client not ready');
    });

    await expect(primeSignedInUser()).resolves.toBeUndefined();
    registry.primeSignedInState(true);

    expect(registry.hasSignedInUser()).toBe(true);
  });

  it('keeps a sign-in that lands while the read is pending over a late empty session', async () => {
    let resolveSession!: (value: unknown) => void;
    const { registry, primeSignedInUser } = freshModules(
      () => new Promise((resolve) => (resolveSession = resolve)),
    );
    const priming = primeSignedInUser();

    registry.notifyIdentityChange('user-1');
    resolveSession(await sessionOf(false));
    await priming;

    expect(registry.hasSignedInUser()).toBe(true);
  });

  it('lets a real sign-in apply while the session read never settles', async () => {
    const { registry, primeSignedInUser } = freshModules(() => new Promise(() => undefined));

    void primeSignedInUser();
    registry.notifyIdentityChange('user-1');

    expect(registry.hasSignedInUser()).toBe(true);
  });

  it('keeps the first answer when two boots prime and the reads settle in opposite order', async () => {
    const resolvers: ((value: unknown) => void)[] = [];
    const { registry, primeSignedInUser } = freshModules(
      () => new Promise((resolve) => resolvers.push(resolve)),
    );
    const first = primeSignedInUser();
    const second = primeSignedInUser();
    const [resolveFirst, resolveSecond] = resolvers as [
      (value: unknown) => void,
      (value: unknown) => void,
    ];

    resolveSecond(await sessionOf(true));
    await second;
    resolveFirst(await sessionOf(false));
    await first;

    expect(registry.hasSignedInUser()).toBe(true);
  });

  it('applies a later successful prime after an earlier read failed', async () => {
    let calls = 0;
    const { registry, primeSignedInUser } = freshModules(() => {
      calls += 1;
      return calls === 1 ? Promise.reject(new Error('storage down')) : sessionOf(true);
    });

    await primeSignedInUser();
    await primeSignedInUser();

    expect(registry.hasSignedInUser()).toBe(true);
  });
});
