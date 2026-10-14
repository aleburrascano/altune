import type { authStorage } from '@shared/auth/authStorage.web';
import { clearPersistedAuthSession } from '@shared/auth/supabaseClient';

const SESSION_KEY = 'sb-fixture-auth-token';
const SESSION_VALUE = JSON.stringify({ access_token: 'a', refresh_token: 'r' });

function loadFreshAuthStorage(): typeof authStorage {
  let loaded: typeof authStorage | undefined;
  jest.isolateModules(() => {
    loaded = require('@shared/auth/authStorage.web').authStorage;
  });
  if (loaded == null) throw new Error('authStorage.web did not load');
  return loaded;
}

afterEach(() => {
  window.localStorage.clear();
});

describe('mh04: the web session survives a reload and sign-out clears it', () => {
  it('keeps a signed-in user signed in across a reload, and leaves no Supabase session in localStorage after sign-out', async () => {
    await loadFreshAuthStorage().setItem(SESSION_KEY, SESSION_VALUE);
    expect(window.localStorage.getItem(SESSION_KEY)).toBe(SESSION_VALUE);

    const reloadedStorage = loadFreshAuthStorage();
    await expect(reloadedStorage.getItem(SESSION_KEY)).resolves.toBe(SESSION_VALUE);

    await clearPersistedAuthSession();

    expect(window.localStorage.getItem(SESSION_KEY)).toBeNull();
  });
});
