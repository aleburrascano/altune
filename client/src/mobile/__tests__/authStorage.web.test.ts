import { createElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';

import { installAuthClient, useSession } from '@shared/auth';
import { createAuthClient, createWebAuthStorage } from '@/mobile';
import type { AuthClientParts } from '@/mobile';

const { __secureStore } = require('expo-secure-store') as { __secureStore: { keys(): string[] } };

const FIXTURE = { url: 'https://fixture.supabase.co', anonKey: 'fixture-anon-key' };

function installWorkingLocalStorage(backing: Map<string, string> = new Map()): Map<string, string> {
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

beforeEach(() => {
  installWorkingLocalStorage();
});

afterEach(() => {
  installWorkingLocalStorage();
});

type WebStorageScenario = { storage: AuthClientParts['storage']; backing: Map<string, string> };

function webStorageUnder(
  scenario: 'with-local-storage' | 'no-window' | 'no-local-storage',
  existingBacking: Map<string, string> = new Map(),
): WebStorageScenario {
  if (scenario === 'no-window') {
    const globalWithWindow = global as unknown as { window?: unknown };
    const originalWindow = globalWithWindow.window;
    delete globalWithWindow.window;
    const storage = createWebAuthStorage();
    globalWithWindow.window = originalWindow;
    return { storage, backing: installWorkingLocalStorage() };
  }
  if (scenario === 'no-local-storage') {
    Object.defineProperty(window, 'localStorage', {
      value: undefined,
      writable: true,
      configurable: true,
      enumerable: true,
    });
    const storage = createWebAuthStorage();
    return { storage, backing: installWorkingLocalStorage() };
  }
  const backing = installWorkingLocalStorage(existingBacking);
  return { storage: createWebAuthStorage(), backing };
}

function clientOver(storage: AuthClientParts['storage']) {
  return createAuthClient(FIXTURE, { storage, fetch: () => new Promise(() => undefined) });
}

const TOKEN_SHAPED_SESSION = JSON.stringify({
  access_token: 'ey.jwt.access-token',
  refresh_token: 'refresh-token-abc',
});

describe('webStorage adapter — the session is kept in window.localStorage so it survives a reload (reverses #945)', () => {
  it('writing the session on web stores it in window.localStorage under the given key', async () => {
    const { storage, backing } = webStorageUnder('with-local-storage');

    await storage.setItem('sb-auth-token', TOKEN_SHAPED_SESSION);

    expect(backing.size).toBe(1);
    expect(backing.get('sb-auth-token')).toBe(TOKEN_SHAPED_SESSION);
  });

  it('a session already sitting in localStorage before the client is constructed is read back by the client', async () => {
    const existing = new Map([['sb-auth-token', TOKEN_SHAPED_SESSION]]);
    const { storage } = webStorageUnder('with-local-storage', existing);

    await expect(storage.getItem('sb-auth-token')).resolves.toBe(TOKEN_SHAPED_SESSION);
  });

  it('reading or removing one key never touches other keys already in localStorage', async () => {
    const existing = new Map([
      ['sb-auth-token', TOKEN_SHAPED_SESSION],
      ['sb-auth-token-code-verifier', 'pkce-verifier'],
      ['unrelated-app-key', 'kept'],
    ]);
    const { storage, backing } = webStorageUnder('with-local-storage', existing);

    await storage.getItem('sb-auth-token');
    await storage.removeItem('sb-auth-token-code-verifier');

    expect([...backing.keys()].sort()).toEqual(['sb-auth-token', 'unrelated-app-key']);
    expect(backing.get('sb-auth-token')).toBe(TOKEN_SHAPED_SESSION);
  });

  it('a localStorage that throws on access (sandboxed iframe, blocked storage) never breaks the in-memory session', async () => {
    const { storage } = webStorageUnder('with-local-storage');
    Object.defineProperty(window, 'localStorage', {
      get: () => {
        throw new Error('SecurityError: storage is blocked');
      },
      configurable: true,
    });

    await storage.setItem('sb-auth-token', TOKEN_SHAPED_SESSION);

    await expect(storage.getItem('sb-auth-token')).resolves.toBe(TOKEN_SHAPED_SESSION);
    await expect(storage.removeItem('sb-auth-token')).resolves.toBeUndefined();
  });

  it('round-trips in memory for the life of the page, so the SDK can still hold and refresh the session', async () => {
    const { storage } = webStorageUnder('with-local-storage');

    await storage.setItem('sb-auth-token', TOKEN_SHAPED_SESSION);
    await expect(storage.getItem('sb-auth-token')).resolves.toBe(TOKEN_SHAPED_SESSION);

    await storage.removeItem('sb-auth-token');
    await expect(storage.getItem('sb-auth-token')).resolves.toBeNull();
  });

  it('survives a page reload — a fresh storage still reads the session written before reload', async () => {
    const sameBrowserProfile = new Map<string, string>();
    const first = webStorageUnder('with-local-storage', sameBrowserProfile);
    await first.storage.setItem('sb-auth-token', TOKEN_SHAPED_SESSION);

    const reloaded = webStorageUnder('with-local-storage', sameBrowserProfile);

    await expect(reloaded.storage.getItem('sb-auth-token')).resolves.toBe(TOKEN_SHAPED_SESSION);
  });

  it('never routes web session writes through the native keychain adapter', async () => {
    const { storage } = webStorageUnder('with-local-storage');

    await storage.setItem('sb-auth-token', TOKEN_SHAPED_SESSION);

    expect(__secureStore.keys()).toEqual([]);
  });
});

describe('webStorage — a static web prerender in Node has no window.localStorage, and construction must not throw', () => {
  it.each([
    ['window itself is absent, as in a Node prerender with no DOM shim at all', 'no-window'],
    ['window exists but its localStorage is unavailable', 'no-local-storage'],
  ] as const)(
    '%s: installs the in-memory adapter and persists nothing to disk',
    async (_label, scenario) => {
      const { storage, backing } = webStorageUnder(scenario);

      await expect(storage.setItem('sb-auth-token', 'value')).resolves.toBeUndefined();
      await expect(storage.getItem('sb-auth-token')).resolves.toBe('value');
      await expect(storage.removeItem('sb-auth-token')).resolves.toBeUndefined();
      await expect(storage.getItem('sb-auth-token')).resolves.toBeNull();
      expect(backing.size).toBe(0);
      expect(__secureStore.keys()).toEqual([]);
    },
  );
});

describe('webStorage adapter — the session survives a page reload (localStorage-backed)', () => {
  it('a localStorage call that throws after construction (private-mode write, quota exceeded) degrades to a no-op rather than crashing', async () => {
    const { storage, backing } = webStorageUnder('with-local-storage');
    const realSetItem = window.localStorage.setItem.bind(window.localStorage);
    Object.defineProperty(window.localStorage, 'setItem', {
      value: () => {
        throw new Error('QuotaExceededError');
      },
      configurable: true,
    });

    await expect(storage.setItem('sb-auth-token', TOKEN_SHAPED_SESSION)).resolves.toBeUndefined();

    Object.defineProperty(window.localStorage, 'setItem', {
      value: realSetItem,
      configurable: true,
    });
    expect(backing.size).toBe(0);
  });

  it('a setItem that soft-fails mid-session still lets getItem return that value for the rest of the session, even once localStorage is reachable again', async () => {
    const { storage } = webStorageUnder('with-local-storage');
    const realSetItem = window.localStorage.setItem.bind(window.localStorage);
    Object.defineProperty(window.localStorage, 'setItem', {
      value: () => {
        throw new Error('QuotaExceededError');
      },
      configurable: true,
    });

    await storage.setItem('sb-auth-token', TOKEN_SHAPED_SESSION);
    Object.defineProperty(window.localStorage, 'setItem', {
      value: realSetItem,
      configurable: true,
    });

    await expect(storage.getItem('sb-auth-token')).resolves.toBe(TOKEN_SHAPED_SESSION);
  });
});

describe('webStorage adapter — a localStorage method that throws (not the localStorage getter itself)', () => {
  it('a localStorage.getItem that throws degrades to null instead of crashing', async () => {
    const { storage } = webStorageUnder('with-local-storage');
    Object.defineProperty(window.localStorage, 'getItem', {
      value: () => {
        throw new Error('SecurityError: storage is blocked');
      },
      configurable: true,
    });

    await expect(storage.getItem('sb-auth-token')).resolves.toBeNull();
  });

  it('a localStorage.removeItem that throws is a no-op on disk but still clears the in-memory fallback', async () => {
    const { storage, backing } = webStorageUnder('with-local-storage');
    const realSetItem = window.localStorage.setItem.bind(window.localStorage);
    Object.defineProperty(window.localStorage, 'setItem', {
      value: () => {
        throw new Error('QuotaExceededError');
      },
      configurable: true,
    });
    await storage.setItem('sb-auth-token', TOKEN_SHAPED_SESSION);
    Object.defineProperty(window.localStorage, 'setItem', {
      value: realSetItem,
      configurable: true,
    });
    expect(backing.size).toBe(0);

    Object.defineProperty(window.localStorage, 'removeItem', {
      value: () => {
        throw new Error('SecurityError: storage is blocked');
      },
      configurable: true,
    });

    await expect(storage.removeItem('sb-auth-token')).resolves.toBeUndefined();

    await expect(storage.getItem('sb-auth-token')).resolves.toBeNull();
  });

  it('window disappearing after construction (not just before it) still resolves reads through the in-memory fallback', async () => {
    const { storage } = webStorageUnder('with-local-storage');
    const globalWithWindow = global as unknown as { window?: unknown };
    const originalWindow = globalWithWindow.window;
    delete globalWithWindow.window;

    await expect(storage.getItem('sb-auth-token')).resolves.toBeNull();

    globalWithWindow.window = originalWindow;
  });
});

describe('clearPersistedSession() — the sign-out guarantee independent of the network call', () => {
  it('removes the persisted auth session key from localStorage on web', async () => {
    const sameBrowserProfile = new Map<string, string>();
    const { storage } = webStorageUnder('with-local-storage', sameBrowserProfile);
    const client = clientOver(storage);
    await client.auth.getSession();
    sameBrowserProfile.set('sb-fixture-auth-token', TOKEN_SHAPED_SESSION);

    await client.clearPersistedSession();

    expect(sameBrowserProfile.has('sb-fixture-auth-token')).toBe(false);
  });

  it('mh04: keeps a signed-in user signed in across a reload, and leaves no Supabase session in localStorage after sign-out', async () => {
    const sameBrowserProfile = new Map<string, string>();
    await webStorageUnder('with-local-storage', sameBrowserProfile).storage.setItem(
      'sb-fixture-auth-token',
      TOKEN_SHAPED_SESSION,
    );
    expect(sameBrowserProfile.get('sb-fixture-auth-token')).toBe(TOKEN_SHAPED_SESSION);

    const reloaded = webStorageUnder('with-local-storage', sameBrowserProfile).storage;
    await expect(reloaded.getItem('sb-fixture-auth-token')).resolves.toBe(TOKEN_SHAPED_SESSION);

    await clientOver(reloaded).clearPersistedSession();

    expect(sameBrowserProfile.has('sb-fixture-auth-token')).toBe(false);
  });
});

describe('the wired real client — a session written by sign-in is readable after a simulated reload (Done when #1)', () => {
  it('a session seeded in localStorage before construction is returned by the real supabase-js client, and useSession reports signed in', async () => {
    const persistedSession = {
      access_token: 'access-token',
      refresh_token: 'refresh-token',
      expires_at: Math.floor(Date.now() / 1000) + 3600,
      expires_in: 3600,
      token_type: 'bearer',
      user: { id: 'user-1' },
    };
    const sameBrowserProfile = new Map<string, string>([
      ['sb-fixture-auth-token', JSON.stringify(persistedSession)],
    ]);
    const client = clientOver(webStorageUnder('with-local-storage', sameBrowserProfile).storage);

    await expect(client.auth.getSession()).resolves.toMatchObject({
      data: { session: { access_token: 'access-token', user: { id: 'user-1' } } },
    });

    installAuthClient(client);
    const queryClient = new QueryClient();
    const { result } = renderHook(() => useSession(), {
      wrapper: ({ children }) =>
        createElement(QueryClientProvider, { client: queryClient }, children),
    });

    await waitFor(() => {
      expect(result.current).toMatchObject({ status: 'signed-in' });
    });
  });
});
