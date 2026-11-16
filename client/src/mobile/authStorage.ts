import * as SecureStore from 'expo-secure-store';

export type AuthStorage = {
  getItem: (key: string) => Promise<string | null>;
  setItem: (key: string, value: string) => Promise<void>;
  removeItem: (key: string) => Promise<void>;
};

const KEYCHAIN_OPTS = { keychainAccessible: SecureStore.AFTER_FIRST_UNLOCK };

type Warn = (message: string, error: unknown) => void;

function warnOnConsole(message: string, error: unknown): void {
  console.warn(message, error);
}

function readKeychain(key: string, warn: Warn): Promise<string | null> {
  return SecureStore.getItemAsync(key, KEYCHAIN_OPTS).catch((error: unknown) => {
    warn('[auth] secure store read failed', error);
    return null;
  });
}

export function createAuthStorage(warn: Warn = warnOnConsole): AuthStorage {
  return {
    getItem: (key: string): Promise<string | null> => readKeychain(key, warn),
    setItem: (key: string, value: string): Promise<void> =>
      SecureStore.setItemAsync(key, value, KEYCHAIN_OPTS),
    removeItem: (key: string): Promise<void> => SecureStore.deleteItemAsync(key, KEYCHAIN_OPTS),
  };
}
