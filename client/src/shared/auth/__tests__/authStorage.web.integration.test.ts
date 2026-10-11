import { createLocalStorageWebStorage } from '@shared/auth/authStorage.web';
import { clearPersistedAuthSession } from '@shared/auth/supabaseClient';

const SESSION_KEY = 'sb-fixture-auth-token';
const SESSION_VALUE = JSON.stringify({ access_token: 'a', refresh_token: 'r' });

afterEach(() => {
  window.localStorage.clear();
});

describe('mh04: the web session survives a reload and sign-out clears it', () => {
  it('keeps a signed-in user signed in across a reload, and leaves no Supabase session in localStorage after sign-out', async () => {
    await createLocalStorageWebStorage().setItem(SESSION_KEY, SESSION_VALUE);
    expect(window.localStorage.getItem(SESSION_KEY)).toBe(SESSION_VALUE);

    const reloadedStorage = createLocalStorageWebStorage();
    await expect(reloadedStorage.getItem(SESSION_KEY)).resolves.toBe(SESSION_VALUE);

    await clearPersistedAuthSession();

    expect(window.localStorage.getItem(SESSION_KEY)).toBeNull();
  });
});
