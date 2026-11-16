import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import * as SecureStore from 'expo-secure-store';

import { createAuthClient, createAuthStorage } from '@/mobile';
import type { AuthClientParts } from '@/mobile';

type SecureStoreDouble = {
  seed(key: string, value: string): void;
  read(key: string): string | undefined;
  keys(): string[];
  failNext(operation: 'get' | 'set' | 'delete', error?: Error): void;
  optionsOf(operation: 'get' | 'set' | 'delete'): unknown[];
};

const { __secureStore } = require('expo-secure-store') as { __secureStore: SecureStoreDouble };

const AUTH_FETCH_TIMEOUT_MS = 10_000;
const MICROTASKS_BEFORE_THE_REQUEST = 10;

const FIXTURE = { url: 'https://fixture.supabase.co', anonKey: 'fixture-anon-key' };

const TOKEN_SHAPED_SESSION = JSON.stringify({
  access_token: 'ey.jwt.access-token',
  refresh_token: 'refresh-token-abc',
});

type Warning = { message: string; error: unknown };

type RecordingStorage = { storage: AuthClientParts['storage']; warnings: Warning[] };

type MemoryParts = AuthClientParts & { removed: string[] };

function recordingStorage(): RecordingStorage {
  const warnings: Warning[] = [];
  const storage = createAuthStorage((message, error) => warnings.push({ message, error }));
  return { storage, warnings };
}

function memoryParts(): MemoryParts {
  const items = new Map<string, string>();
  const removed: string[] = [];
  return {
    removed,
    storage: {
      getItem: (key) => Promise.resolve(items.get(key) ?? null),
      setItem: (key, value) => Promise.resolve(void items.set(key, value)),
      removeItem: (key) => {
        removed.push(key);
        items.delete(key);
        return Promise.resolve();
      },
    },
    fetch: () => new Promise<Response>(() => undefined),
  };
}

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

afterEach(() => {
  installWorkingLocalStorage();
});

describe('secureStoreAdapter — persistence round-trip', () => {
  it('setItem then getItem returns exactly the value that was written', async () => {
    const storage = createAuthStorage();

    await storage.setItem('sb-auth-token', 'the-serialized-session-json');

    await expect(storage.getItem('sb-auth-token')).resolves.toBe('the-serialized-session-json');
  });

  it('removeItem deletes the entry outright — the key is gone, not blanked to an empty string', async () => {
    const storage = createAuthStorage();
    await storage.setItem('sb-auth-token', 'the-serialized-session-json');

    await storage.removeItem('sb-auth-token');

    expect(__secureStore.keys()).not.toContain('sb-auth-token');
    expect(__secureStore.read('sb-auth-token')).toBeUndefined();
    await expect(storage.getItem('sb-auth-token')).resolves.toBeNull();
  });
});

describe('secureStoreAdapter — failure injection at every keychain call site', () => {
  it('getItem degrades to null when the keychain read throws, so the SDK re-authenticates instead of crashing', async () => {
    const { storage, warnings } = recordingStorage();
    const failure = new Error('keychain unavailable');
    __secureStore.failNext('get', failure);

    await expect(storage.getItem('sb-auth-token')).resolves.toBeNull();
    expect(warnings).toEqual([{ message: '[auth] secure store read failed', error: failure }]);
  });

  it('setItem propagates the keychain write failure rather than swallowing it', async () => {
    const storage = createAuthStorage();
    __secureStore.failNext('set', new Error('keychain unavailable'));

    await expect(storage.setItem('sb-auth-token', 'value')).rejects.toThrow('keychain unavailable');
  });

  it('removeItem propagates the keychain delete failure rather than swallowing it', async () => {
    const storage = createAuthStorage();
    __secureStore.failNext('delete', new Error('keychain unavailable'));

    await expect(storage.removeItem('sb-auth-token')).rejects.toThrow('keychain unavailable');
  });
});

describe('secureStoreAdapter — legacy shapes an older app version could have left behind', () => {
  it('a key that was never written resolves to null', async () => {
    await expect(createAuthStorage().getItem('never-written-key')).resolves.toBeNull();
  });

  it('a value that is not the JSON the SDK expects still comes back verbatim, because the adapter never parses it', async () => {
    __secureStore.seed('sb-auth-token', 'not-json-at-all-from-an-older-build');

    await expect(createAuthStorage().getItem('sb-auth-token')).resolves.toBe(
      'not-json-at-all-from-an-older-build',
    );
  });

  it('an item written under a different keychain accessibility option throws on read and degrades to null, not a crash', async () => {
    const { storage } = recordingStorage();
    __secureStore.seed('sb-auth-token', '{"access_token":"old-sdk-session"}');
    __secureStore.failNext(
      'get',
      new Error('item was stored under a different keychainAccessible option'),
    );

    await expect(storage.getItem('sb-auth-token')).resolves.toBeNull();
  });
});

describe('secureStoreAdapter — adversarial disk boundary: the value is opaque, never parsed or validated', () => {
  it.each([
    ['a thin, empty-string value', ''],
    ['a partial, truncated JSON fragment', '{"access_token":"abc123","expires_a'],
    ['a tampered value carrying an embedded control character', 'abc\u0000def'],
    ['a malformed, non-JSON-shaped value', '[object Object]'],
  ])('%s round-trips byte-for-byte', async (_label, value) => {
    const storage = createAuthStorage();

    await storage.setItem('sb-auth-token', value);

    await expect(storage.getItem('sb-auth-token')).resolves.toBe(value);
  });
});

describe('secureStoreAdapter — security: the keychain accessibility option', () => {
  it('passes the same AFTER_FIRST_UNLOCK option to getItemAsync, setItemAsync and deleteItemAsync alike', async () => {
    const storage = createAuthStorage();

    await storage.setItem('sb-auth-token', 'v');
    await storage.getItem('sb-auth-token');
    await storage.removeItem('sb-auth-token');

    const expected = { keychainAccessible: SecureStore.AFTER_FIRST_UNLOCK };
    expect(__secureStore.optionsOf('set')).toEqual([expected]);
    expect(__secureStore.optionsOf('get')).toEqual([expected]);
    expect(__secureStore.optionsOf('delete')).toEqual([expected]);
  });

  it('never routes a token-shaped value through a warning', async () => {
    const { storage, warnings } = recordingStorage();

    await storage.setItem('sb-auth-token', TOKEN_SHAPED_SESSION);
    await storage.getItem('sb-auth-token');
    await storage.removeItem('sb-auth-token');

    expect(warnings).toEqual([]);
  });
});

describe("createAuthClient() — the client's configured session policy", () => {
  it('survives an app restart, refreshes silently before the access token expires, and never lets the SDK hijack the URL on a deep link', () => {
    const parts = memoryParts();

    const { auth } = createAuthClient(FIXTURE, parts);

    expect(auth).toMatchObject({
      storage: parts.storage,
      persistSession: true,
      autoRefreshToken: true,
      detectSessionInUrl: false,
      flowType: 'pkce',
    });
  });

  it('reads a session persisted before startup back from the storage it was given', async () => {
    const parts = memoryParts();
    const persisted = {
      access_token: 'access-token',
      refresh_token: 'refresh-token',
      expires_at: Math.floor(Date.now() / 1000) + 3600,
      expires_in: 3600,
      token_type: 'bearer',
      user: { id: 'user-1' },
    };
    await parts.storage.setItem('sb-fixture-auth-token', JSON.stringify(persisted));

    const { auth } = createAuthClient(FIXTURE, parts);

    await expect(auth.getSession()).resolves.toMatchObject({
      data: { session: { access_token: 'access-token', user: { id: 'user-1' } } },
    });
  });
});

describe('createAuthClient() — every auth request runs within the fetch deadline', () => {
  afterEach(() => {
    jest.useRealTimers();
  });

  it('hands the auth SDK a fetch that aborts a request the server never answers', async () => {
    const parts = memoryParts();
    const signals: AbortSignal[] = [];
    parts.fetch = (_input, init) => {
      if (init?.signal) signals.push(init.signal);
      return new Promise<Response>(() => undefined);
    };
    const { auth } = createAuthClient(FIXTURE, parts);
    await auth.getSession();
    jest.useFakeTimers();

    void auth.signInWithPassword({ email: 'a@b.co', password: 'pw' });
    for (let tick = 0; tick < MICROTASKS_BEFORE_THE_REQUEST && signals.length === 0; tick += 1) {
      await Promise.resolve();
    }
    expect(signals).toHaveLength(1);
    expect(signals[0]?.aborted).toBe(false);

    jest.advanceTimersByTime(AUTH_FETCH_TIMEOUT_MS);

    expect(signals[0]?.aborted).toBe(true);
  });
});

describe('native storage — a look-alike window.localStorage is never consulted', () => {
  it('reads and writes go to the keychain with 0 localStorage calls', async () => {
    const getItem = jest.fn(() => 'from-local-storage');
    const setItem = jest.fn();
    const removeItem = jest.fn();
    Object.defineProperty(window, 'localStorage', {
      value: { getItem, setItem, removeItem },
      writable: true,
      configurable: true,
      enumerable: true,
    });
    const storage = createAuthStorage();

    await storage.setItem('sb-fixture-auth-token', 'v');
    await expect(storage.getItem('sb-fixture-auth-token')).resolves.toBe('v');
    await storage.removeItem('sb-fixture-auth-token');

    expect(getItem).toHaveBeenCalledTimes(0);
    expect(setItem).toHaveBeenCalledTimes(0);
    expect(removeItem).toHaveBeenCalledTimes(0);
  });
});

describe('clearPersistedSession() — the sign-out guarantee independent of the network call', () => {
  it('removes the persisted auth session key, named after the project host, from the storage', async () => {
    const parts = memoryParts();
    const client = createAuthClient(FIXTURE, parts);
    await client.auth.getSession();

    await client.clearPersistedSession();

    expect(parts.removed).toEqual(['sb-fixture-auth-token']);
  });

  it('removes the persisted auth session key from the keychain on native', async () => {
    const client = createAuthClient(FIXTURE, { ...memoryParts(), storage: createAuthStorage() });
    await client.auth.getSession();
    __secureStore.seed('sb-fixture-auth-token', TOKEN_SHAPED_SESSION);

    await client.clearPersistedSession();

    expect(__secureStore.keys()).not.toContain('sb-fixture-auth-token');
  });
});

describe('required configuration', () => {
  const ORIGINAL_URL = process.env.EXPO_PUBLIC_SUPABASE_URL;

  afterEach(() => {
    process.env.EXPO_PUBLIC_SUPABASE_URL = ORIGINAL_URL;
  });

  describe('createAuthClient — required configuration fails fast at app startup', () => {
    it.each([
      ['EXPO_PUBLIC_SUPABASE_URL', { ...FIXTURE, url: undefined }],
      ['EXPO_PUBLIC_SUPABASE_ANON_KEY', { ...FIXTURE, anonKey: undefined }],
    ])('throws naming %s when it is unset', (name, config) => {
      expect(() => createAuthClient(config, memoryParts())).toThrow(name);
    });

    it.each([
      ['EXPO_PUBLIC_SUPABASE_URL', { ...FIXTURE, url: '' }],
      ['EXPO_PUBLIC_SUPABASE_ANON_KEY', { ...FIXTURE, anonKey: '' }],
    ])('throws naming %s when it is the empty string', (name, config) => {
      expect(() => createAuthClient(config, memoryParts())).toThrow(name);
    });

    it('reads the app environment when no configuration is passed in', () => {
      expect(() => createAuthClient()).not.toThrow();

      process.env.EXPO_PUBLIC_SUPABASE_URL = '';

      expect(() => createAuthClient()).toThrow('EXPO_PUBLIC_SUPABASE_URL');
    });
  });

  describe('the documented environment', () => {
    it('lists every EXPO_PUBLIC variable the web export needs in client/.env.example', () => {
      const example = readFileSync(join(__dirname, '../../../.env.example'), 'utf8');

      const keys = example
        .split('\n')
        .filter((line) => line.length > 0)
        .map((line) => line.split('=')[0]);

      expect(keys).toEqual([
        'EXPO_PUBLIC_SUPABASE_URL',
        'EXPO_PUBLIC_SUPABASE_ANON_KEY',
        'EXPO_PUBLIC_API_URL',
      ]);
    });
  });
});
