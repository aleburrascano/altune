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

function apiBaseAfterStartupWith(apiUrl: string): string {
  const unset = process.env.EXPO_PUBLIC_API_URL;
  process.env.EXPO_PUBLIC_API_URL = apiUrl;
  let base = '';
  try {
    jest.isolateModules(() => {
      require('@/mobile');
      base = require('@shared/apiConfig').apiBase();
    });
  } finally {
    if (unset === undefined) delete process.env.EXPO_PUBLIC_API_URL;
    else process.env.EXPO_PUBLIC_API_URL = unset;
  }
  return base;
}

describe('RootLayout — the app installs the API base from EXPO_PUBLIC_API_URL at startup', () => {
  it('sends requests to the base the build was given', () => {
    expect(apiBaseAfterStartupWith('https://altune.example.org')).toBe(
      'https://altune.example.org',
    );
  });
});
