type StorageAdapter = {
  getItem: (key: string) => Promise<string | null>;
  setItem: (key: string, value: string) => Promise<void>;
  removeItem: (key: string) => Promise<void>;
};

type SecureStoreDouble = {
  AFTER_FIRST_UNLOCK: string;
  getItemAsync: (...args: unknown[]) => Promise<string | null>;
  setItemAsync: (...args: unknown[]) => Promise<void>;
  deleteItemAsync: (...args: unknown[]) => Promise<void>;
  __secureStore: {
    reset(): void;
    seed(key: string, value: string): void;
    read(key: string): string | undefined;
    keys(): string[];
    failNext(operation: 'get' | 'set' | 'delete' | 'unavailable', error?: Error): void;
  };
};

type CapturedAuthOptions = {
  auth: {
    storage: StorageAdapter;
    persistSession: boolean;
    autoRefreshToken: boolean;
    detectSessionInUrl: boolean;
  };
};

let capturedOptions: CapturedAuthOptions | undefined;

jest.mock('@supabase/supabase-js', () => ({
  createClient: (_url: string, _key: string, options: CapturedAuthOptions): { __fake: true } => {
    capturedOptions = options;
    return { __fake: true };
  },
}));

type Recorded = { getItemAsync: unknown[][]; setItemAsync: unknown[][]; deleteItemAsync: unknown[][] };

function freshModules(): {
  storage: StorageAdapter;
  SecureStore: SecureStoreDouble;
  calls: Recorded;
  authOptions: CapturedAuthOptions;
} {
  jest.resetModules();
  capturedOptions = undefined;

  const SecureStore = require('expo-secure-store') as SecureStoreDouble;
  SecureStore.__secureStore.reset();

  const calls: Recorded = { getItemAsync: [], setItemAsync: [], deleteItemAsync: [] };
  const originalGet = SecureStore.getItemAsync;
  const originalSet = SecureStore.setItemAsync;
  const originalDelete = SecureStore.deleteItemAsync;
  SecureStore.getItemAsync = (...args: unknown[]) => {
    calls.getItemAsync.push(args);
    return originalGet(...args);
  };
  SecureStore.setItemAsync = (...args: unknown[]) => {
    calls.setItemAsync.push(args);
    return originalSet(...args);
  };
  SecureStore.deleteItemAsync = (...args: unknown[]) => {
    calls.deleteItemAsync.push(args);
    return originalDelete(...args);
  };

  require('../supabaseClient');
  const options = capturedOptions as CapturedAuthOptions | undefined;
  if (!options) throw new Error('createClient was never called — construction did not happen');

  return { storage: options.auth.storage, SecureStore, calls, authOptions: options };
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

beforeEach(() => {
  installWorkingLocalStorage();
});

afterEach(() => {
  const RN = require('react-native') as { Platform: { OS: string } };
  RN.Platform.OS = 'ios';
  installWorkingLocalStorage();
  jest.resetModules();
});

describe('secureStoreAdapter — persistence round-trip', () => {
  it('setItem then getItem returns exactly the value that was written', async () => {
    const { storage } = freshModules();

    await storage.setItem('sb-auth-token', 'the-serialized-session-json');

    await expect(storage.getItem('sb-auth-token')).resolves.toBe('the-serialized-session-json');
  });

  it('removeItem deletes the entry outright — the key is gone, not blanked to an empty string', async () => {
    const { storage, SecureStore } = freshModules();
    await storage.setItem('sb-auth-token', 'the-serialized-session-json');

    await storage.removeItem('sb-auth-token');

    expect(SecureStore.__secureStore.keys()).not.toContain('sb-auth-token');
    expect(SecureStore.__secureStore.read('sb-auth-token')).toBeUndefined();
    await expect(storage.getItem('sb-auth-token')).resolves.toBeNull();
  });
});

describe('secureStoreAdapter — failure injection at every keychain call site', () => {
  it('getItem degrades to null when the keychain read throws, so the SDK re-authenticates instead of crashing', async () => {
    const { storage, SecureStore } = freshModules();
    SecureStore.__secureStore.failNext('get', new Error('keychain unavailable'));

    await expect(storage.getItem('sb-auth-token')).resolves.toBeNull();
  });

  it('setItem propagates the keychain write failure rather than swallowing it', async () => {
    const { storage, SecureStore } = freshModules();
    SecureStore.__secureStore.failNext('set', new Error('keychain unavailable'));

    await expect(storage.setItem('sb-auth-token', 'value')).rejects.toThrow('keychain unavailable');
  });

  it('removeItem propagates the keychain delete failure rather than swallowing it', async () => {
    const { storage, SecureStore } = freshModules();
    SecureStore.__secureStore.failNext('delete', new Error('keychain unavailable'));

    await expect(storage.removeItem('sb-auth-token')).rejects.toThrow('keychain unavailable');
  });
});

describe('secureStoreAdapter — legacy shapes an older app version could have left behind', () => {
  it('a key that was never written resolves to null', async () => {
    const { storage } = freshModules();

    await expect(storage.getItem('never-written-key')).resolves.toBeNull();
  });

  it('a value that is not the JSON the SDK expects still comes back verbatim, because the adapter never parses it', async () => {
    const { storage, SecureStore } = freshModules();
    SecureStore.__secureStore.seed('sb-auth-token', 'not-json-at-all-from-an-older-build');

    await expect(storage.getItem('sb-auth-token')).resolves.toBe('not-json-at-all-from-an-older-build');
  });

  it('an item written under a different keychain accessibility option throws on read and degrades to null, not a crash', async () => {
    const { storage, SecureStore } = freshModules();
    SecureStore.__secureStore.seed('sb-auth-token', '{"access_token":"old-sdk-session"}');
    SecureStore.__secureStore.failNext(
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
    const { storage } = freshModules();

    await storage.setItem('sb-auth-token', value);

    await expect(storage.getItem('sb-auth-token')).resolves.toBe(value);
  });
});

describe('secureStoreAdapter — security: the keychain accessibility option', () => {
  it('passes the same AFTER_FIRST_UNLOCK option to getItemAsync, setItemAsync and deleteItemAsync alike', async () => {
    const { storage, SecureStore, calls } = freshModules();

    await storage.setItem('sb-auth-token', 'v');
    await storage.getItem('sb-auth-token');
    await storage.removeItem('sb-auth-token');

    const expected = { keychainAccessible: SecureStore.AFTER_FIRST_UNLOCK };
    expect(calls.setItemAsync[0]?.[2]).toEqual(expected);
    expect(calls.getItemAsync[0]?.[1]).toEqual(expected);
    expect(calls.deleteItemAsync[0]?.[1]).toEqual(expected);
  });

  it('never routes a token-shaped value through console.log, console.warn or console.error', async () => {
    const { storage } = freshModules();
    const logSpy = jest.spyOn(console, 'log').mockImplementation(() => undefined);
    const warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    const errorSpy = jest.spyOn(console, 'error').mockImplementation(() => undefined);
    const tokenShaped = JSON.stringify({ access_token: 'ey.jwt.token', refresh_token: 'refresh-abc' });

    await storage.setItem('sb-auth-token', tokenShaped);
    await storage.getItem('sb-auth-token');
    await storage.removeItem('sb-auth-token');

    expect(logSpy).not.toHaveBeenCalled();
    expect(warnSpy).not.toHaveBeenCalled();
    expect(errorSpy).not.toHaveBeenCalled();

    logSpy.mockRestore();
    warnSpy.mockRestore();
    errorSpy.mockRestore();
  });
});

describe("secureStoreAdapter — the client's configured session policy", () => {
  it('survives an app restart, refreshes silently before the access token expires, and never lets the SDK hijack the URL on a deep link', () => {
    const { authOptions } = freshModules();

    expect(authOptions.auth.persistSession).toBe(true);
    expect(authOptions.auth.autoRefreshToken).toBe(true);
    expect(authOptions.auth.detectSessionInUrl).toBe(false);
    expect(typeof authOptions.auth.storage.getItem).toBe('function');
    expect(typeof authOptions.auth.storage.setItem).toBe('function');
    expect(typeof authOptions.auth.storage.removeItem).toBe('function');
  });
});

function webStorageUnder(
  scenario: 'with-local-storage' | 'no-window' | 'no-local-storage',
  existingBacking: Map<string, string> = new Map(),
): {
  storage: StorageAdapter;
  backing: Map<string, string>;
  SecureStore: SecureStoreDouble;
} {
  capturedOptions = undefined;
  let SecureStore: SecureStoreDouble | undefined;
  let backing = new Map<string, string>();

  jest.isolateModules(() => {
    const RN = require('react-native') as { Platform: { OS: string } };
    RN.Platform.OS = 'web';
    SecureStore = require('expo-secure-store') as SecureStoreDouble;
    SecureStore.__secureStore.reset();

    if (scenario === 'no-window') {
      const globalWithWindow = global as unknown as { window?: unknown };
      const originalWindow = globalWithWindow.window;
      delete globalWithWindow.window;
      require('../supabaseClient');
      globalWithWindow.window = originalWindow;
      backing = installWorkingLocalStorage();
      return;
    }
    if (scenario === 'no-local-storage') {
      Object.defineProperty(window, 'localStorage', {
        value: undefined,
        writable: true,
        configurable: true,
        enumerable: true,
      });
      require('../supabaseClient');
      backing = installWorkingLocalStorage();
      return;
    }
    backing = installWorkingLocalStorage(existingBacking);
    require('../supabaseClient');
  });

  const options = capturedOptions as CapturedAuthOptions | undefined;
  if (!options || !SecureStore) throw new Error('createClient was never called — construction did not happen');
  return { storage: options.auth.storage, backing, SecureStore };
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

  it('survives a page reload — a fresh module instance still reads the session written before reload', async () => {
    const sameBrowserProfile = new Map<string, string>();
    const first = webStorageUnder('with-local-storage', sameBrowserProfile);
    await first.storage.setItem('sb-auth-token', TOKEN_SHAPED_SESSION);

    const reloaded = webStorageUnder('with-local-storage', sameBrowserProfile);

    await expect(reloaded.storage.getItem('sb-auth-token')).resolves.toBe(TOKEN_SHAPED_SESSION);
  });

  it('never routes web session writes through the native keychain adapter', async () => {
    const { storage, SecureStore } = webStorageUnder('with-local-storage');

    await storage.setItem('sb-auth-token', TOKEN_SHAPED_SESSION);

    expect(SecureStore.__secureStore.keys()).toEqual([]);
  });
});

describe('webStorage — a static web prerender in Node has no window.localStorage, and construction must not throw', () => {
  it.each([
    ['window itself is absent, as in a Node prerender with no DOM shim at all', 'no-window'],
    ['window exists but its localStorage is unavailable', 'no-local-storage'],
  ] as const)('%s: installs the in-memory adapter and persists nothing to disk', async (_label, scenario) => {
    const { storage, backing, SecureStore } = webStorageUnder(scenario);

    await expect(storage.setItem('sb-auth-token', 'value')).resolves.toBeUndefined();
    await expect(storage.getItem('sb-auth-token')).resolves.toBe('value');
    await expect(storage.removeItem('sb-auth-token')).resolves.toBeUndefined();
    await expect(storage.getItem('sb-auth-token')).resolves.toBeNull();
    expect(backing.size).toBe(0);
    expect(SecureStore.__secureStore.keys()).toEqual([]);
  });
});

describe('webStorage adapter — the session survives a page reload (localStorage-backed)', () => {
  it('a session written before a simulated reload is readable by a freshly constructed client afterwards', async () => {
    const sameBrowserProfile = new Map<string, string>();
    const beforeReload = webStorageUnder('with-local-storage', sameBrowserProfile);
    await beforeReload.storage.setItem('sb-auth-token', TOKEN_SHAPED_SESSION);

    const afterReload = webStorageUnder('with-local-storage', sameBrowserProfile);

    await expect(afterReload.storage.getItem('sb-auth-token')).resolves.toBe(TOKEN_SHAPED_SESSION);
  });

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

    Object.defineProperty(window.localStorage, 'setItem', { value: realSetItem, configurable: true });
    expect(backing.size).toBe(0);
  });
});

describe('clearPersistedAuthSession() — the sign-out guarantee independent of the network call', () => {
  it('removes the persisted auth session key from localStorage on web', async () => {
    const sameBrowserProfile = new Map<string, string>([['sb-fixture-auth-token', TOKEN_SHAPED_SESSION]]);
    let clearPersistedAuthSession: (() => Promise<void>) | undefined;

    jest.isolateModules(() => {
      const RN = require('react-native') as { Platform: { OS: string } };
      RN.Platform.OS = 'web';
      installWorkingLocalStorage(sameBrowserProfile);
      ({ clearPersistedAuthSession } = require('../supabaseClient') as {
        clearPersistedAuthSession: () => Promise<void>;
      });
    });

    await clearPersistedAuthSession!();

    expect(sameBrowserProfile.has('sb-fixture-auth-token')).toBe(false);
  });

  it('removes the persisted auth session key from the keychain on native', async () => {
    const { SecureStore, calls } = freshModules();
    await SecureStore.__secureStore.seed('sb-fixture-auth-token', TOKEN_SHAPED_SESSION);

    const { clearPersistedAuthSession } = require('../supabaseClient') as {
      clearPersistedAuthSession: () => Promise<void>;
    };
    await clearPersistedAuthSession();

    expect(calls.deleteItemAsync.length).toBeGreaterThan(0);
    expect(SecureStore.__secureStore.keys()).not.toContain('sb-fixture-auth-token');
  });
});
