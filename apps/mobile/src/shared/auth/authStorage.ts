import * as SecureStore from 'expo-secure-store';

type AuthStorage = {
  getItem: (key: string) => Promise<string | null>;
  setItem: (key: string, value: string) => Promise<void>;
  removeItem: (key: string) => Promise<void>;
};

const KEYCHAIN_OPTS = { keychainAccessible: SecureStore.AFTER_FIRST_UNLOCK };

export const authStorage: AuthStorage = {
  getItem: (key: string): Promise<string | null> =>
    SecureStore.getItemAsync(key, KEYCHAIN_OPTS).catch(() => null),
  setItem: (key: string, value: string): Promise<void> =>
    SecureStore.setItemAsync(key, value, KEYCHAIN_OPTS),
  removeItem: (key: string): Promise<void> => SecureStore.deleteItemAsync(key, KEYCHAIN_OPTS),
};
