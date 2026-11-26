import { renderHook, act } from '@testing-library/react-native';

import { supabase } from '@shared/auth';

import {
  _resetConsumedCredentialForTest,
  completeAuthIntent,
  parseAuthLink,
  OAUTH_BROWSER_TIMEOUT_MS,
  useOAuth,
} from '@features/signIn';
import {
  authPortWrapper,
  createAuthBrowserMock,
  createAuthPortMock,
  type AuthBrowserMock,
  createSupabaseAuthMock,
} from '@features/signIn/__tests__/testUtils/authTestUtils';
const AUTH_ACTION_TIMEOUT_MS = 20_000;

const { signInWithOAuth, exchangeCodeForSession } = createSupabaseAuthMock(
  'signInWithOAuth',
  'exchangeCodeForSession',
);
const authPort = createAuthPortMock();
let browser: AuthBrowserMock;

const REDIRECT_URL = 'altune://auth/callback?code=abc';
const REDIRECT_CODE = 'abc';
const CODELESS_REDIRECT_URL = 'altune://auth/callback';
const NO_SESSION = { session: null, user: null };

function exchangeSucceeds(): void {
  exchangeCodeForSession.mockResolvedValue({ data: NO_SESSION, error: null });
}

function exchangeFailsWith(error: unknown): void {
  exchangeCodeForSession.mockResolvedValue({ data: NO_SESSION, error });
}

function listenerSpendsTheRedirect(): Promise<unknown> {
  return completeAuthIntent(parseAuthLink(REDIRECT_URL), { replace: jest.fn() }, supabase.auth);
}

beforeEach(() => {
  browser = createAuthBrowserMock();
  authPort.reportSignInFailure.mockClear();
  _resetConsumedCredentialForTest();
  exchangeSucceeds();
});

function grantAuthorizationUrl(): void {
  signInWithOAuth.mockResolvedValue({
    data: { url: 'https://accounts.google.com/o' },
    error: null,
  });
}

async function signIn(): Promise<{ kind: string }> {
  const { result } = renderHook(() => useOAuth(), { wrapper: authPortWrapper(authPort, browser) });
  await act(async () => {
    await result.current.signInWith('google');
  });
  return result.current.state;
}

describe('useOAuth: deriving the terminal state from the real exchange outcome (#657)', () => {
  beforeEach(() => {
    grantAuthorizationUrl();
    browser.openAuthSession.mockReset().mockResolvedValue({ type: 'success', url: REDIRECT_URL });
  });

  it('reports ok when Supabase accepts the code exchange', async () => {
    expect(await signIn()).toEqual({ kind: 'ok' });
    expect(authPort.reportSignInFailure).not.toHaveBeenCalled();
    expect(exchangeCodeForSession).toHaveBeenCalledWith(REDIRECT_CODE);
  });

  it('reports error instead of a false ok when the redirect carries no code to exchange', async () => {
    browser.openAuthSession.mockResolvedValue({ type: 'success', url: CODELESS_REDIRECT_URL });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'unknown' });
    expect(authPort.reportSignInFailure).toHaveBeenCalledWith('unknown');
  });

  it('reports a network error when the exchange fails with a 503', async () => {
    exchangeFailsWith({ status: 503 });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'network' });
  });

  it('reports a network error when the exchange fails with an offline status 0', async () => {
    exchangeFailsWith({ status: 0 });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'network' });
  });

  it('reports an unknown error when the exchange fails with invalid_grant', async () => {
    exchangeFailsWith({ status: 400, code: 'invalid_grant' });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'unknown' });
  });

  it('treats a deduped callback as ok because the deep-link listener established the session', async () => {
    await listenerSpendsTheRedirect();

    expect(await signIn()).toEqual({ kind: 'ok' });
    expect(exchangeCodeForSession).toHaveBeenCalledTimes(1);
  });

  it('reports error when signInWithOAuth returns { error } before the browser opens', async () => {
    signInWithOAuth.mockResolvedValue({ data: null, error: { message: 'nope' } });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'unknown' });
    expect(browser.openAuthSession).not.toHaveBeenCalled();
  });

  it('reports cancelled when the user dismisses the auth browser', async () => {
    browser.openAuthSession.mockResolvedValue({ type: 'cancel' });

    expect(await signIn()).toEqual({ kind: 'cancelled' });
    expect(exchangeCodeForSession).not.toHaveBeenCalled();
  });
});

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
    grantAuthorizationUrl();
    browser.openAuthSession.mockReset().mockResolvedValue({ type: 'success', url: REDIRECT_URL });
  });

  afterEach(() => {
    jest.useRealTimers();
    jest.restoreAllMocks();
  });

  it('abandons a signInWithOAuth call that never resolves at the auth budget', async () => {
    signInWithOAuth.mockReturnValue(neverSettles());
    const { result } = renderHook(() => useOAuth(), {
      wrapper: authPortWrapper(authPort, browser),
    });

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
    browser.openAuthSession.mockReturnValue(neverSettles());
    const { result } = renderHook(() => useOAuth(), {
      wrapper: authPortWrapper(authPort, browser),
    });

    let call!: Promise<void>;
    act(() => {
      call = result.current.signInWith('google');
    });
    await flushPendingWork();

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

  it('dismisses the auth session once when the browser wait times out', async () => {
    browser.dismissAuthSession.mockReset();
    browser.openAuthSession.mockReturnValue(neverSettles());
    const { result } = renderHook(() => useOAuth(), {
      wrapper: authPortWrapper(authPort, browser),
    });

    let call!: Promise<void>;
    act(() => {
      call = result.current.signInWith('google');
    });
    await flushPendingWork();
    expect(browser.dismissAuthSession).not.toHaveBeenCalled();

    await act(async () => {
      jest.advanceTimersByTime(OAUTH_BROWSER_TIMEOUT_MS);
      await call;
    });
    expect(browser.dismissAuthSession).toHaveBeenCalledTimes(1);
    expect(result.current.state).toEqual({ kind: 'cancelled' });
  });

  it('still spends the redirect once the screen that started the sign-in has unmounted', async () => {
    const browserSession = deferredBrowserSession();
    browser.openAuthSession.mockReturnValue(browserSession.promise);
    const { result, unmount } = renderHook(() => useOAuth(), {
      wrapper: authPortWrapper(authPort, browser),
    });

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
    expect(exchangeCodeForSession).toHaveBeenCalledWith(REDIRECT_CODE);
  });

  it('classifies a transport failure from the provider call as a network error', async () => {
    signInWithOAuth.mockResolvedValue({
      data: null,
      error: { name: 'AuthRetryableFetchError', status: 0, message: 'failed to fetch' },
    });
    const { result } = renderHook(() => useOAuth(), {
      wrapper: authPortWrapper(authPort, browser),
    });

    await act(async () => {
      await result.current.signInWith('google');
    });

    expect(result.current.state).toEqual({ kind: 'error', reason: 'network' });
  });

  it('keeps an abandoned leg that later rejects from becoming an unhandled rejection', async () => {
    const unhandled = jest.fn();
    process.on('unhandledRejection', unhandled);
    let rejectBrowser!: (error: Error) => void;
    browser.openAuthSession.mockReturnValue(
      new Promise((_resolve, reject) => {
        rejectBrowser = reject;
      }),
    );
    const { result } = renderHook(() => useOAuth(), {
      wrapper: authPortWrapper(authPort, browser),
    });

    let call!: Promise<void>;
    act(() => {
      call = result.current.signInWith('google');
    });
    await flushPendingWork();
    await act(async () => {
      jest.advanceTimersByTime(OAUTH_BROWSER_TIMEOUT_MS);
      await call;
    });

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
    browser.openAuthSession.mockReset().mockResolvedValue({ type: 'success', url: REDIRECT_URL });
  });

  it('opens one browser session when a second press lands before the first settles', async () => {
    const browserSession = deferredBrowserSession();
    browser.openAuthSession.mockReturnValue(browserSession.promise);
    const { result } = renderHook(() => useOAuth(), {
      wrapper: authPortWrapper(authPort, browser),
    });

    let presses!: Promise<unknown>;
    act(() => {
      presses = Promise.all([
        result.current.signInWith('google'),
        result.current.signInWith('google'),
      ]);
    });
    await flushPendingWork();

    expect(signInWithOAuth).toHaveBeenCalledTimes(1);
    expect(browser.openAuthSession).toHaveBeenCalledTimes(1);

    await act(async () => {
      browserSession.resolve({ type: 'success', url: REDIRECT_URL });
      await presses;
    });
    expect(exchangeCodeForSession).toHaveBeenCalledTimes(1);
    expect(result.current.state).toEqual({ kind: 'ok' });
  });

  it('lets the next press through once the first sign-in has settled', async () => {
    const { result } = renderHook(() => useOAuth(), {
      wrapper: authPortWrapper(authPort, browser),
    });

    await act(async () => {
      await result.current.signInWith('google');
    });
    await act(async () => {
      await result.current.signInWith('google');
    });

    expect(signInWithOAuth).toHaveBeenCalledTimes(2);
  });
});

describe('useOAuth: a server rate limit is not a network error', () => {
  beforeEach(() => {
    signInWithOAuth.mockResolvedValue({
      data: { url: 'https://accounts.google.com/o' },
      error: null,
    });
    browser.openAuthSession.mockReset().mockResolvedValue({
      type: 'success',
      url: 'altune://auth/callback?code=abc',
    });
  });

  it('maps a 429 on the authorization request to too_many_attempts', async () => {
    signInWithOAuth.mockResolvedValue({
      data: null,
      error: { status: 429, code: 'over_request_rate_limit' },
    });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'too_many_attempts' });
  });

  it('maps a 429 on the code exchange to too_many_attempts', async () => {
    exchangeFailsWith({ status: 429, code: 'over_request_rate_limit' });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'too_many_attempts' });
  });

  it('still maps a 503 on the authorization request to network', async () => {
    signInWithOAuth.mockResolvedValue({ data: null, error: { status: 503 } });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'network' });
  });
});

describe('useOAuth: the authorization request on native, precisely (#2837)', () => {
  it('asks Supabase for the altune-scheme redirect with skipBrowserRedirect, then hands that same url to the in-app browser', async () => {
    grantAuthorizationUrl();
    browser.openAuthSession.mockReset().mockResolvedValue({ type: 'success', url: REDIRECT_URL });

    await signIn();

    expect(signInWithOAuth).toHaveBeenCalledWith({
      provider: 'google',
      options: { redirectTo: 'altune://auth/callback', skipBrowserRedirect: true },
    });
    expect(browser.openAuthSession).toHaveBeenCalledWith(
      'https://accounts.google.com/o',
      'altune://auth/callback',
    );
  });
});

const reportedReasons = (): string[] =>
  authPort.reportSignInFailure.mock.calls.map(([reason]) => reason);

describe('useOAuth: reporting a failed sign-in to the anonymous ingest', () => {
  beforeEach(() => {
    grantAuthorizationUrl();
    browser.openAuthSession.mockReset().mockResolvedValue({ type: 'success', url: REDIRECT_URL });
  });

  it('reports the error reason once when the exchange fails', async () => {
    exchangeFailsWith({ status: 503 });

    await signIn();

    expect(reportedReasons()).toEqual(['network']);
  });
});

describe('useOAuth: a cancelled browser is not a failure to report', () => {
  beforeEach(() => {
    grantAuthorizationUrl();
    exchangeFailsWith({ status: 400, code: 'invalid_grant' });
  });

  it('adds no report for a cancel after an error was reported', async () => {
    browser.openAuthSession.mockReset().mockResolvedValue({ type: 'success', url: REDIRECT_URL });
    await signIn();
    browser.openAuthSession.mockResolvedValue({ type: 'cancel' });
    await signIn();

    expect(reportedReasons()).toEqual(['unknown']);
  });
});
