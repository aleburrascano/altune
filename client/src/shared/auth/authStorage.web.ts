type AuthStorage = {
  getItem: (key: string) => Promise<string | null>;
  setItem: (key: string, value: string) => Promise<void>;
  removeItem: (key: string) => Promise<void>;
};

function reachableLocalStorage(): Storage | null {
  if (typeof window === 'undefined') return null;
  try {
    return window.localStorage ?? null;
  } catch {
    return null;
  }
}

function readWebStorage(fallback: Map<string, string>, key: string): Promise<string | null> {
  if (fallback.has(key)) return Promise.resolve(fallback.get(key) ?? null);
  const localStorage = reachableLocalStorage();
  if (localStorage == null) return Promise.resolve(null);
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
  const localStorage = reachableLocalStorage();
  if (localStorage == null || !trySetItem(localStorage, key, value)) {
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
  const localStorage = reachableLocalStorage();
  if (localStorage != null) tryRemoveItem(localStorage, key);
  return Promise.resolve();
}

function createLocalStorageWebStorage(): AuthStorage {
  const unreachableFallback = new Map<string, string>();

  return {
    getItem: (key) => readWebStorage(unreachableFallback, key),
    setItem: (key, value) => writeWebStorage(unreachableFallback, key, value),
    removeItem: (key) => deleteFromWebStorage(unreachableFallback, key),
  };
}

export const authStorage = createLocalStorageWebStorage();
