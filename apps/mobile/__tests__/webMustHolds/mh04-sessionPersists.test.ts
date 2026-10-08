function installFakeLocalStorage(backing: Map<string, string> = new Map()): Map<string, string> {
  Object.defineProperty(window, 'localStorage', {
    value: {
      getItem: (key: string): string | null => (backing.has(key) ? backing.get(key)! : null),
      setItem: (key: string, value: string): void => {
        backing.set(key, String(value));
      },
      removeItem: (key: string): void => {
        backing.delete(key);
      },
    },
    writable: true,
    configurable: true,
    enumerable: true,
  });
  return backing;
}

afterEach(() => {
  jest.dontMock('@shared/auth/authStorage');
  installFakeLocalStorage();
  jest.resetModules();
});

const SESSION_KEY = 'sb-fixture-auth-token';
const SESSION_VALUE = JSON.stringify({ access_token: 'a', refresh_token: 'r' });

describe('mh04: the web session survives a reload and sign-out clears it', () => {
  it('keeps a signed-in user signed in across a reload, and leaves no Supabase session in localStorage after sign-out', async () => {
    const sameBrowserProfile = new Map<string, string>();

    let storage: {
      getItem: (key: string) => Promise<string | null>;
      setItem: (key: string, value: string) => Promise<void>;
    };
    let clearPersistedAuthSession: () => Promise<void>;

    jest.isolateModules(() => {
      jest.doMock('@shared/auth/authStorage', () =>
        jest.requireActual('@shared/auth/authStorage.web'),
      );
      installFakeLocalStorage(sameBrowserProfile);
      const web = require('@shared/auth/authStorage.web') as {
        createLocalStorageWebStorage: () => typeof storage;
      };
      const mod = require('@shared/auth/supabaseClient') as {
        clearPersistedAuthSession: () => Promise<void>;
      };
      storage = web.createLocalStorageWebStorage();
      clearPersistedAuthSession = mod.clearPersistedAuthSession;
    });

    await storage!.setItem(SESSION_KEY, SESSION_VALUE);
    expect(sameBrowserProfile.get(SESSION_KEY)).toBe(SESSION_VALUE);

    let reloadedStorage: { getItem: (key: string) => Promise<string | null> };
    jest.isolateModules(() => {
      jest.doMock('@shared/auth/authStorage', () =>
        jest.requireActual('@shared/auth/authStorage.web'),
      );
      installFakeLocalStorage(sameBrowserProfile);
      const web = require('@shared/auth/authStorage.web') as {
        createLocalStorageWebStorage: () => typeof reloadedStorage;
      };
      reloadedStorage = web.createLocalStorageWebStorage();
    });

    await expect(reloadedStorage!.getItem(SESSION_KEY)).resolves.toBe(SESSION_VALUE);

    await clearPersistedAuthSession!();

    expect(sameBrowserProfile.has(SESSION_KEY)).toBe(false);
  });
});
