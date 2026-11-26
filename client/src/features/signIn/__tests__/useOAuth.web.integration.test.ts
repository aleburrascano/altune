import { renderHook, act } from '@testing-library/react-native';

import { useOAuth } from '@features/signIn';
import {
  authPortWrapper,
  createAuthBrowserMock,
  createAuthPortMock,
  createSupabaseAuthMock,
} from '@features/signIn/__tests__/testUtils/authTestUtils';

const AUTH_ACTION_TIMEOUT_MS = 20_000;

const authPort = createAuthPortMock();
const { signInWithOAuth } = createSupabaseAuthMock('signInWithOAuth');

beforeEach(() => {
  authPort.reportSignInFailure.mockClear();
});
const browser = createAuthBrowserMock();

function neverSettles<T>(): Promise<T> {
  return new Promise<T>(() => {});
}

async function signIn(): Promise<{ kind: string }> {
  const { result } = renderHook(() => useOAuth(), { wrapper: authPortWrapper(authPort, browser) });
  await act(async () => {
    await result.current.signInWith('google');
  });
  return result.current.state;
}

describe('useOAuth: a full-page redirect on web, not a popup (#2837)', () => {
  it('sends signInWithOAuth a same-origin redirectTo and never opens the in-app browser', async () => {
    signInWithOAuth.mockResolvedValue({
      data: { url: 'https://accounts.google.com/o' },
      error: null,
    });

    await signIn();

    expect(signInWithOAuth).toHaveBeenCalledWith({
      provider: 'google',
      options: { redirectTo: `${window.location.origin}/auth/callback` },
    });
    expect(browser.openAuthSession).not.toHaveBeenCalled();
  });

  it('reports the failure when the redirect request itself is refused', async () => {
    signInWithOAuth.mockResolvedValue({ data: null, error: { message: 'nope' } });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'unknown' });
    expect(authPort.reportSignInFailure).toHaveBeenCalledWith('unknown');
  });
});

describe('useOAuth: pending survives a successful redirect, since the tab is on its way out (#2837)', () => {
  it('leaves state at pending on a successful redirect request', async () => {
    signInWithOAuth.mockResolvedValue({
      data: { url: 'https://accounts.google.com/o' },
      error: null,
    });
    const { result } = renderHook(() => useOAuth(), {
      wrapper: authPortWrapper(authPort, browser),
    });

    await act(async () => {
      await result.current.signInWith('google');
    });

    expect(result.current.state).toEqual({ kind: 'pending', provider: 'google' });
  });

  it('still reports a refused redirect once the screen that started it has unmounted', async () => {
    let resolveSignIn!: (value: { data: null; error: { message: string } }) => void;
    signInWithOAuth.mockReturnValue(
      new Promise((settle) => {
        resolveSignIn = settle;
      }),
    );
    const { result, unmount } = renderHook(() => useOAuth(), {
      wrapper: authPortWrapper(authPort, browser),
    });

    let call!: Promise<void>;
    act(() => {
      call = result.current.signInWith('google');
    });
    unmount();
    await act(async () => {
      resolveSignIn({ data: null, error: { message: 'nope' } });
      await call;
    });

    expect(authPort.reportSignInFailure).toHaveBeenCalledWith('unknown');
  });
});

describe('useOAuth: a rejected web redirect request settles into error, not pending forever (#2837)', () => {
  afterEach(() => {
    jest.useRealTimers();
  });

  it('reports a network error when signInWithOAuth rejects on web', async () => {
    signInWithOAuth.mockRejectedValue(new TypeError('Failed to fetch'));

    expect(await signIn()).toEqual({ kind: 'error', reason: 'network' });
    expect(authPort.reportSignInFailure).toHaveBeenCalledWith('network');
  });

  it('reports an unknown error when signInWithOAuth rejects on web with something other than a network failure', async () => {
    signInWithOAuth.mockRejectedValue(new Error('boom'));

    expect(await signIn()).toEqual({ kind: 'error', reason: 'unknown' });
  });

  it('reports a network error when the web redirect request stalls past the auth deadline', async () => {
    jest.useFakeTimers();
    signInWithOAuth.mockReturnValue(neverSettles());
    const { result } = renderHook(() => useOAuth(), {
      wrapper: authPortWrapper(authPort, browser),
    });

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
  });
});

const reportedReasons = (): string[] =>
  authPort.reportSignInFailure.mock.calls.map(([reason]) => reason);

describe('useOAuth (web): reporting a failed redirect to the anonymous ingest', () => {
  it('reports the error reason once, and nothing when the redirect starts', async () => {
    signInWithOAuth.mockResolvedValue({ data: null, error: { message: 'nope' } });
    await signIn();
    expect(reportedReasons()).toEqual(['unknown']);

    authPort.reportSignInFailure.mockClear();
    signInWithOAuth.mockResolvedValue({ data: { url: 'https://x' }, error: null });
    await signIn();
    expect(authPort.reportSignInFailure).not.toHaveBeenCalled();
  });
});
