import { Platform } from 'react-native';

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

const STARTS_WITH_THE_SHORT_COMMIT = /^0123456789ab\//;

function afterStartupWith<T>(env: Record<string, string>, read: () => T): T {
  const previous = Object.fromEntries(Object.keys(env).map((name) => [name, process.env[name]]));
  Object.assign(process.env, env);
  let value!: T;
  try {
    jest.isolateModules(() => {
      require('@/mobile');
      value = read();
    });
  } finally {
    for (const [name, before] of Object.entries(previous)) {
      if (before === undefined) delete process.env[name];
      else process.env[name] = before;
    }
  }
  return value;
}

describe('RootLayout — the app installs the API base from EXPO_PUBLIC_API_URL at startup', () => {
  it('sends requests to the base the build was given', () => {
    const base = afterStartupWith({ EXPO_PUBLIC_API_URL: 'https://altune.example.org' }, () =>
      require('@shared/apiConfig').apiBase(),
    );
    expect(base).toBe('https://altune.example.org');
  });
});

describe('RootLayout — the app installs its build from EXPO_PUBLIC_BUILD_COMMIT at startup', () => {
  it('names the build commit in the telemetry version', () => {
    const version = afterStartupWith({ EXPO_PUBLIC_BUILD_COMMIT: '0123456789abcdef' }, () =>
      require('@shared/device').telemetryVersion(),
    );
    expect(version).toMatch(STARTS_WITH_THE_SHORT_COMMIT);
  });

  it('names the platform the app runs on', () => {
    const platform = afterStartupWith({}, () => require('@shared/device').platformName());
    expect(platform).toBe(Platform.OS);
  });
});
