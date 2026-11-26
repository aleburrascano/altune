import { browserStorage } from '@/mobile/adapters/web/browserStorage';

export type AuthStorage = {
  getItem: (key: string) => Promise<string | null>;
  setItem: (key: string, value: string) => Promise<void>;
  removeItem: (key: string) => Promise<void>;
};

function readWebStorage(fallback: Map<string, string>, key: string): Promise<string | null> {
  if (fallback.has(key)) return Promise.resolve(fallback.get(key) ?? null);
  const localStorage = browserStorage();
  if (localStorage === undefined) return Promise.resolve(null);
  try {
    return Promise.resolve(localStorage.getItem(key));
  } catch {
    return Promise.resolve(null);
  }
}

function trySetItem(localStorage: Storage, key: string, value: string): boolean {
  try {
    localStorage.setItem(key, value);
    return true;
  } catch {
    return false;
  }
}

function writeWebStorage(fallback: Map<string, string>, key: string, value: string): Promise<void> {
  const localStorage = browserStorage();
  if (localStorage === undefined || !trySetItem(localStorage, key, value)) {
    fallback.set(key, value);
    return Promise.resolve();
  }
  fallback.delete(key);
  return Promise.resolve();
}

function tryRemoveItem(localStorage: Storage, key: string): void {
  try {
    localStorage.removeItem(key);
  } catch {
    return;
  }
}

function deleteFromWebStorage(fallback: Map<string, string>, key: string): Promise<void> {
  fallback.delete(key);
  const localStorage = browserStorage();
  if (localStorage !== undefined) tryRemoveItem(localStorage, key);
  return Promise.resolve();
}

export function createAuthStorage(): AuthStorage {
  const unreachableFallback = new Map<string, string>();

  return {
    getItem: (key) => readWebStorage(unreachableFallback, key),
    setItem: (key, value) => writeWebStorage(unreachableFallback, key, value),
    removeItem: (key) => deleteFromWebStorage(unreachableFallback, key),
  };
}
