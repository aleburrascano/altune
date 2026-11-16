import { supabase } from '@shared/auth';
import { RootLayout } from '@/mobile';

const { __http } = require('@testing/doubles/fetch.js');

const AUTH_FETCH_TIMEOUT_MS = 10_000;

afterEach(() => {
  jest.useRealTimers();
});

describe('RootLayout — the app installs its own auth client at startup', () => {
  it('hands auth the client the app built, whose requests give up at the fetch deadline', async () => {
    expect(RootLayout).toEqual(expect.any(Function));
    await supabase.auth.getSession();
    jest.useFakeTimers();
    __http.hang('POST /auth/v1/token');

    const attempt = supabase.auth.signInWithPassword({ email: 'a@b.co', password: 'pw' });
    await jest.advanceTimersByTimeAsync(AUTH_FETCH_TIMEOUT_MS);

    await expect(attempt).resolves.toMatchObject({
      error: { name: 'AuthRetryableFetchError' },
    });
  });
});
