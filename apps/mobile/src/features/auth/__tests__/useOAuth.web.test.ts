import { renderHook, act } from '@testing-library/react-native';
import * as WebBrowser from 'expo-web-browser';
import { Platform } from 'react-native';

import { AUTH_ACTION_TIMEOUT_MS } from '../authDeadline';
import { useOAuth } from '../hooks/useOAuth.web';

import { createSupabaseAuthMock } from './testUtils/authTestUtils';

const mockStateUpdates: unknown[] = [];

jest.mock('react', () => {
  const actual = jest.requireActual('react');
  return {
    ...actual,
    useState: (initial: unknown) => {
      const state = actual.useState(initial);
      return [
        state[0],
        (next: unknown) => {
          mockStateUpdates.push(next);
          state[1](next);
        },
      ];
    },
  };
});
jest.mock('expo-web-browser', () => ({
  maybeCompleteAuthSession: jest.fn(),
  openAuthSessionAsync: jest.fn(),
}));
jest.mock('@shared/auth/supabaseClient', () => ({ supabase: { auth: {} } }));

const { signInWithOAuth } = createSupabaseAuthMock('signInWithOAuth');
const openAuthSessionAsync = WebBrowser.openAuthSessionAsync as unknown as jest.Mock;

function neverSettles<T>(): Promise<T> {
  return new Promise<T>(() => {});
}

async function signIn(): Promise<{ kind: string }> {
  const { result } = renderHook(() => useOAuth());
  await act(async () => {
    await result.current.signInWith('google');
  });
  return result.current.state;
}

describe('useOAuth: a full-page redirect on web, not a popup (#2837)', () => {
  afterEach(() => {
    Platform.OS = 'ios';
    Reflect.deleteProperty(globalThis, 'window');
  });

  it('sends signInWithOAuth a same-origin redirectTo and never opens the in-app browser', async () => {
    Platform.OS = 'web';
    Object.assign(globalThis, { window: { location: { origin: 'https://app.altune.example' } } });
    signInWithOAuth.mockResolvedValue({
      data: { url: 'https://accounts.google.com/o' },
      error: null,
    });

    await signIn();

    expect(signInWithOAuth).toHaveBeenCalledWith({
      provider: 'google',
      options: { redirectTo: 'https://app.altune.example/auth/callback' },
    });
    expect(openAuthSessionAsync).not.toHaveBeenCalled();
  });

  it('reports the failure when the redirect request itself is refused', async () => {
    Platform.OS = 'web';
    Object.assign(globalThis, { window: { location: { origin: 'https://app.altune.example' } } });
    signInWithOAuth.mockResolvedValue({ data: null, error: { message: 'nope' } });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'unknown' });
  });
});

describe('useOAuth: pending survives a successful redirect, since the tab is on its way out (#2837)', () => {
  afterEach(() => {
    Platform.OS = 'ios';
    Reflect.deleteProperty(globalThis, 'window');
  });

  it('leaves state at pending on a successful redirect request', async () => {
    Platform.OS = 'web';
    Object.assign(globalThis, { window: { location: { origin: 'https://app.altune.example' } } });
    signInWithOAuth.mockResolvedValue({
      data: { url: 'https://accounts.google.com/o' },
      error: null,
    });
    const { result } = renderHook(() => useOAuth());

    await act(async () => {
      await result.current.signInWith('google');
    });

    expect(result.current.state).toEqual({ kind: 'pending', provider: 'google' });
  });

  it('sets no state for a refused redirect once the screen that started it has unmounted', async () => {
    Platform.OS = 'web';
    Object.assign(globalThis, { window: { location: { origin: 'https://app.altune.example' } } });
    mockStateUpdates.length = 0;
    let resolveSignIn!: (value: { data: null; error: { message: string } }) => void;
    signInWithOAuth.mockReturnValue(
      new Promise((settle) => {
        resolveSignIn = settle;
      }),
    );
    const { result, unmount } = renderHook(() => useOAuth());

    let call!: Promise<void>;
    act(() => {
      call = result.current.signInWith('google');
    });
    unmount();
    await act(async () => {
      resolveSignIn({ data: null, error: { message: 'nope' } });
      await call;
    });

    expect(mockStateUpdates).toEqual([{ kind: 'pending', provider: 'google' }]);
  });
});

describe('useOAuth: a rejected web redirect request settles into error, not pending forever (#2837)', () => {
  afterEach(() => {
    Platform.OS = 'ios';
    Reflect.deleteProperty(globalThis, 'window');
  });

  it('reports a network error when signInWithOAuth rejects on web', async () => {
    Platform.OS = 'web';
    Object.assign(globalThis, { window: { location: { origin: 'https://app.altune.example' } } });
    signInWithOAuth.mockRejectedValue(new Error('failed to fetch'));

    expect(await signIn()).toEqual({ kind: 'error', reason: 'network' });
  });

  it('reports an unknown error when signInWithOAuth rejects on web with something other than a network failure', async () => {
    Platform.OS = 'web';
    Object.assign(globalThis, { window: { location: { origin: 'https://app.altune.example' } } });
    signInWithOAuth.mockRejectedValue(new Error('boom'));

    expect(await signIn()).toEqual({ kind: 'error', reason: 'unknown' });
  });

  it('reports a network error when the web redirect request stalls past the auth deadline', async () => {
    jest.useFakeTimers();
    Platform.OS = 'web';
    Object.assign(globalThis, { window: { location: { origin: 'https://app.altune.example' } } });
    signInWithOAuth.mockReturnValue(neverSettles());
    const { result } = renderHook(() => useOAuth());

    let call!: Promise<void>;
    act(() => {
      call = result.current.signInWith('google');
    });
    expect(result.current.state).toEqual({ kind: 'pending', provider: 'google' });

    await act(async () => {
      jest.advanceTimersByTime(AUTH_ACTION_TIMEOUT_MS);
      await call;
    });

    expect(result.current.state).toEqual({ kind: 'error', reason: 'network' });
    jest.useRealTimers();
  });
});

const reportFetch = jest.fn();
const realFetch = global.fetch;

beforeEach(() => {
  reportFetch.mockReset().mockResolvedValue({ status: 204 });
  global.fetch = reportFetch as unknown as typeof fetch;
});

afterEach(() => {
  global.fetch = realFetch;
});

const reportedReasons = (): string[] =>
  reportFetch.mock.calls.map(([, init]) => JSON.parse(init.body).reason);

describe('useOAuth (web): reporting a failed redirect to the anonymous ingest', () => {
  afterEach(() => {
    Platform.OS = 'ios';
    Reflect.deleteProperty(globalThis, 'window');
  });

  it('reports the error reason once, and nothing when the redirect starts', async () => {
    Platform.OS = 'web';
    Object.assign(globalThis, { window: { location: { origin: 'https://app.altune.example' } } });
    signInWithOAuth.mockResolvedValue({ data: null, error: { message: 'nope' } });
    await signIn();
    expect(reportedReasons()).toEqual(['unknown']);

    reportFetch.mockClear();
    signInWithOAuth.mockResolvedValue({ data: { url: 'https://x' }, error: null });
    await signIn();
    expect(reportFetch).not.toHaveBeenCalled();
  });
});
