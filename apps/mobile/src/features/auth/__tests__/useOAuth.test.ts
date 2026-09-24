import { renderHook, act } from '@testing-library/react-native';
import * as WebBrowser from 'expo-web-browser';

import { AUTH_ACTION_TIMEOUT_MS } from '../authDeadline';
import { completeAuthIntent } from '../completeAuthIntent';
import { OAUTH_BROWSER_TIMEOUT_MS, useOAuth } from '../hooks/useOAuth';

import { createSupabaseAuthMock } from './testUtils/authTestUtils';

/** Every value the hook passed to its `useState` setter, newest last. */
const mockStateUpdates: unknown[] = [];

// React drops an update to an unmounted hook silently, so the setter is the only
// place the unmount guard is observable. The wrapper has to sit on the module
// itself: the hook's `import { useState }` binding is read once, at import.
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
jest.mock('expo-router', () => ({ useRouter: () => ({ replace: jest.fn() }) }));
jest.mock('expo-web-browser', () => ({
  maybeCompleteAuthSession: jest.fn(),
  openAuthSessionAsync: jest.fn(),
}));
jest.mock('@shared/auth/supabaseClient', () => ({ supabase: { auth: {} } }));
jest.mock('../completeAuthIntent', () => ({ completeAuthIntent: jest.fn() }));

const { signInWithOAuth } = createSupabaseAuthMock('signInWithOAuth');
const openAuthSessionAsync = WebBrowser.openAuthSessionAsync as unknown as jest.Mock;
const mockComplete = completeAuthIntent as jest.Mock;

const REDIRECT_URL = 'altune://auth/callback?code=abc';

function grantAuthorizationUrl(): void {
  signInWithOAuth.mockResolvedValue({
    data: { url: 'https://accounts.google.com/o' },
    error: null,
  });
}

async function signIn(): Promise<{ kind: string }> {
  const { result } = renderHook(() => useOAuth());
  await act(async () => {
    await result.current.signInWith('google');
  });
  return result.current.state;
}

describe('useOAuth: deriving the terminal state from the real exchange outcome (#657)', () => {
  beforeEach(() => {
    grantAuthorizationUrl();
    openAuthSessionAsync.mockReset().mockResolvedValue({ type: 'success', url: REDIRECT_URL });
    mockComplete.mockReset().mockResolvedValue({ kind: 'success' });
  });

  it('reports ok when completeAuthIntent confirms a successful exchange', async () => {
    mockComplete.mockResolvedValue({ kind: 'success' });

    expect(await signIn()).toEqual({ kind: 'ok' });
  });

  it('reports error instead of a false ok when the exchange fails', async () => {
    mockComplete.mockResolvedValue({ kind: 'failure' });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'unknown' });
  });

  it.each([
    ['a 503', { name: 'AuthRetryableFetchError', status: 503, message: 'down' }, 'network'],
    [
      'an offline status 0',
      { name: 'AuthRetryableFetchError', status: 0, message: 'x' },
      'network',
    ],
    [
      'invalid_grant',
      { name: 'AuthApiError', status: 400, code: 'invalid_grant', message: 'x' },
      'unknown',
    ],
  ])('maps %s from the exchange to %s', async (_label, error, reason) => {
    mockComplete.mockResolvedValue({ kind: 'failure', cause: 'gotrue_rejected', error });

    expect(await signIn()).toEqual({ kind: 'error', reason });
  });

  it('treats a deduped callback as ok because the deep-link listener established the session', async () => {
    mockComplete.mockResolvedValue({ kind: 'deduped' });

    expect(await signIn()).toEqual({ kind: 'ok' });
  });

  it('reports error when signInWithOAuth returns { error } before the browser opens', async () => {
    signInWithOAuth.mockResolvedValue({ data: null, error: { message: 'nope' } });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'unknown' });
    expect(openAuthSessionAsync).not.toHaveBeenCalled();
  });

  it('reports cancelled when the user dismisses the auth browser', async () => {
    openAuthSessionAsync.mockResolvedValue({ type: 'cancel' });

    expect(await signIn()).toEqual({ kind: 'cancelled' });
    expect(mockComplete).not.toHaveBeenCalled();
  });
});

/** Settles the promise chain the flow is parked on without advancing any timer. */
async function flushPendingWork(): Promise<void> {
  await act(async () => {
    for (let tick = 0; tick < 10; tick += 1) await Promise.resolve();
  });
}

function neverSettles<T>(): Promise<T> {
  return new Promise<T>(() => {});
}

type BrowserSession = { type: string; url: string };

function deferredBrowserSession(): {
  promise: Promise<BrowserSession>;
  resolve: (session: BrowserSession) => void;
} {
  let resolve!: (session: BrowserSession) => void;
  const promise = new Promise<BrowserSession>((settle) => {
    resolve = settle;
  });
  return { promise, resolve };
}

describe('useOAuth: bounding, cancelling and classifying the flow (#1642)', () => {
  beforeEach(() => {
    jest.useFakeTimers();
    mockStateUpdates.length = 0;
    grantAuthorizationUrl();
    openAuthSessionAsync.mockReset().mockResolvedValue({ type: 'success', url: REDIRECT_URL });
    mockComplete.mockReset().mockResolvedValue({ kind: 'success' });
  });

  afterEach(() => {
    jest.useRealTimers();
    jest.restoreAllMocks();
  });

  it('abandons a signInWithOAuth call that never resolves at the auth budget', async () => {
    signInWithOAuth.mockReturnValue(neverSettles());
    const { result } = renderHook(() => useOAuth());

    let call!: Promise<void>;
    act(() => {
      call = result.current.signInWith('google');
    });
    expect(result.current.state).toEqual({ kind: 'pending', provider: 'google' });

    act(() => {
      jest.advanceTimersByTime(AUTH_ACTION_TIMEOUT_MS - 1);
    });
    expect(result.current.state).toEqual({ kind: 'pending', provider: 'google' });

    await act(async () => {
      jest.advanceTimersByTime(1);
      await call;
    });
    expect(result.current.state).toEqual({ kind: 'error', reason: 'network' });
  });

  it('waits out a human at the provider but abandons a browser session left open', async () => {
    openAuthSessionAsync.mockReturnValue(neverSettles());
    const { result } = renderHook(() => useOAuth());

    let call!: Promise<void>;
    act(() => {
      call = result.current.signInWith('google');
    });
    await flushPendingWork();

    // Signing in at the provider routinely outlasts the SDK budget.
    act(() => {
      jest.advanceTimersByTime(AUTH_ACTION_TIMEOUT_MS);
    });
    await flushPendingWork();
    expect(result.current.state).toEqual({ kind: 'pending', provider: 'google' });

    await act(async () => {
      jest.advanceTimersByTime(OAUTH_BROWSER_TIMEOUT_MS - AUTH_ACTION_TIMEOUT_MS);
      await call;
    });
    expect(result.current.state).toEqual({ kind: 'cancelled' });
  });

  it('sets no state once the screen that started the sign-in has unmounted', async () => {
    const browserSession = deferredBrowserSession();
    openAuthSessionAsync.mockReturnValue(browserSession.promise);
    const { result, unmount } = renderHook(() => useOAuth());

    let call!: Promise<void>;
    act(() => {
      call = result.current.signInWith('google');
    });
    await flushPendingWork();
    unmount();

    await act(async () => {
      browserSession.resolve({ type: 'success', url: REDIRECT_URL });
      await call;
    });
    // The redirect is still exchanged — the session is worth having — but the
    // `ok` it produces has nowhere to go.
    expect(mockComplete).toHaveBeenCalledTimes(1);
    expect(mockStateUpdates).toEqual([{ kind: 'pending', provider: 'google' }]);
  });

  it('classifies a transport failure from the provider call as a network error', async () => {
    signInWithOAuth.mockResolvedValue({
      data: null,
      error: { name: 'AuthRetryableFetchError', status: 0, message: 'failed to fetch' },
    });
    const { result } = renderHook(() => useOAuth());

    await act(async () => {
      await result.current.signInWith('google');
    });

    expect(result.current.state).toEqual({ kind: 'error', reason: 'network' });
  });

  it('keeps an abandoned leg that later rejects from becoming an unhandled rejection', async () => {
    const unhandled = jest.fn();
    process.on('unhandledRejection', unhandled);
    let rejectBrowser!: (error: Error) => void;
    openAuthSessionAsync.mockReturnValue(
      new Promise((_resolve, reject) => {
        rejectBrowser = reject;
      }),
    );
    const { result } = renderHook(() => useOAuth());

    let call!: Promise<void>;
    act(() => {
      call = result.current.signInWith('google');
    });
    await flushPendingWork();
    await act(async () => {
      jest.advanceTimersByTime(OAUTH_BROWSER_TIMEOUT_MS);
      await call;
    });

    // The browser only fails once the user comes back to a dead session, long
    // after the deadline stopped waiting for it.
    rejectBrowser(new Error('no browser is open'));
    jest.useRealTimers();
    await new Promise((resolve) => setImmediate(resolve));

    expect(unhandled).not.toHaveBeenCalled();
    process.off('unhandledRejection', unhandled);
  });
});

describe('useOAuth: rejecting a duplicate press at the hook (#1643)', () => {
  beforeEach(() => {
    grantAuthorizationUrl();
    openAuthSessionAsync.mockReset().mockResolvedValue({ type: 'success', url: REDIRECT_URL });
    mockComplete.mockReset().mockResolvedValue({ kind: 'success' });
  });

  it('opens one browser session when a second press lands before the first settles', async () => {
    const browserSession = deferredBrowserSession();
    openAuthSessionAsync.mockReturnValue(browserSession.promise);
    const { result } = renderHook(() => useOAuth());

    let presses!: Promise<unknown>;
    act(() => {
      presses = Promise.all([
        result.current.signInWith('google'),
        result.current.signInWith('google'),
      ]);
    });
    await flushPendingWork();

    expect(signInWithOAuth).toHaveBeenCalledTimes(1);
    expect(openAuthSessionAsync).toHaveBeenCalledTimes(1);

    await act(async () => {
      browserSession.resolve({ type: 'success', url: REDIRECT_URL });
      await presses;
    });
    expect(mockComplete).toHaveBeenCalledTimes(1);
    expect(result.current.state).toEqual({ kind: 'ok' });
  });

  it('lets the next press through once the first sign-in has settled', async () => {
    const { result } = renderHook(() => useOAuth());

    await act(async () => {
      await result.current.signInWith('google');
    });
    await act(async () => {
      await result.current.signInWith('google');
    });

    expect(signInWithOAuth).toHaveBeenCalledTimes(2);
  });
});
