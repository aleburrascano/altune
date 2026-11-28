import { act, screen } from '@testing-library/react-native';
import { Stack } from 'expo-router';
import { renderRouter } from 'expo-router/testing-library';
import { StrictMode, useSyncExternalStore, type ReactElement, type ReactNode } from 'react';
import { Text } from 'react-native';

import { AuthCallbackScreen } from '@features/signIn/view/AuthCallbackScreen';
import {
  authPortWrapper,
  createSupabaseAuthMock,
  resetConsumedCredential,
} from '@features/signIn/__tests__/authTestUtils';
import { recordLogs, type RecordedLogs } from '@testing/fakes/log/inMemoryLogger';

const AUTH_ACTION_TIMEOUT_MS = 20_000;
const TURN_MS = 50;
const MAX_TURNS = 40;

const { exchangeCodeForSession, verifyOtp, getSession } = createSupabaseAuthMock(
  'exchangeCodeForSession',
  'verifyOtp',
  'getSession',
);

const BAD_REQUEST = 400;
const FORBIDDEN = 403;

const SIGNED_IN = { data: { session: {}, user: { id: 'user-1' } }, error: null };

const CALLBACK_PATH = '/auth/callback';
function Layout(): ReactElement {
  return (
    <Stack screenOptions={{ headerShown: false }}>
      <Stack.Screen name="auth/callback" />
      <Stack.Screen name="auth/confirm" />
      <Stack.Screen name="auth/recovery" />
      <Stack.Screen name="library" />
      <Stack.Screen name="reset-password" />
    </Stack>
  );
}

const ROUTES = {
  _layout: Layout,
  'auth/callback': AuthCallbackScreen,
  'auth/confirm': AuthCallbackScreen,
  'auth/recovery': AuthCallbackScreen,
  library: () => <Text>library-screen</Text>,
  'reset-password': () => <Text>reset-password-screen</Text>,
};

function rejectedBy(code: string, status: number) {
  return {
    data: { session: null, user: null },
    error: { name: 'AuthApiError', message: 'rejected', status, code },
  };
}

type ExchangeReply = typeof SIGNED_IN | ReturnType<typeof rejectedBy>;

function holdExchange(): (reply: ExchangeReply) => void {
  let release: ((reply: ExchangeReply) => void) | null = null;
  exchangeCodeForSession.mockImplementation(
    () => new Promise<ExchangeReply>((resolve) => (release = resolve)),
  );
  return (reply) => release?.(reply);
}

type AddressBar = { writes: string[]; restore(): void };

function watchAddressBar(): AddressBar {
  const original = window.history.replaceState.bind(window.history);
  const writes: string[] = [];
  window.history.replaceState = (state: unknown, unused: string, url?: string | URL | null) => {
    writes.push(String(url));
    original(state, unused, url);
  };
  return {
    writes,
    restore: () => {
      window.history.replaceState = original;
    },
  };
}

let logs: RecordedLogs;
let addressBar: AddressBar;

function openPage(path: string): ReturnType<typeof renderRouter> {
  window.history.pushState({}, '', path);
  addressBar = watchAddressBar();
  return renderRouter(ROUTES, { initialUrl: path, wrapper: Ports });
}

beforeEach(() => {
  logs = recordLogs();
  addressBar = { writes: [], restore: () => undefined };
  resetConsumedCredential();
  exchangeCodeForSession.mockResolvedValue(SIGNED_IN);
  verifyOtp.mockResolvedValue(SIGNED_IN);
  getSession.mockResolvedValue({ data: { session: null }, error: null });
});

afterEach(() => {
  addressBar.restore();
});

async function advance(ms: number): Promise<void> {
  await act(async () => {
    await jest.advanceTimersByTimeAsync(ms);
  });
}

async function eventually(check: () => void): Promise<void> {
  for (let turn = 0; turn < MAX_TURNS; turn += 1) {
    try {
      check();
      return;
    } catch {
      await advance(TURN_MS);
    }
  }
  check();
}

const Ports = authPortWrapper();

const rerenderListeners = new Set<() => void>();
let rerenderVersion = 0;

const rerenders = {
  subscribe: (listener: () => void) => {
    rerenderListeners.add(listener);
    return () => rerenderListeners.delete(listener);
  },
  version: () => rerenderVersion,
  bump: () => {
    rerenderVersion += 1;
    for (const listener of rerenderListeners) listener();
  },
};

function RerenderingCallback(): ReactElement {
  useSyncExternalStore(rerenders.subscribe, rerenders.version);
  return <AuthCallbackScreen />;
}

interface PortsProps {
  children: ReactNode;
}

function StrictPorts({ children }: PortsProps): ReactElement {
  return (
    <StrictMode>
      <Ports>{children}</Ports>
    </StrictMode>
  );
}

function shows(text: string): boolean {
  return renderedText().includes(text);
}

function renderedText(): string {
  return JSON.stringify(screen.toJSON());
}

function lastPath(router: ReturnType<typeof renderRouter>): string {
  return router.getPathname();
}

describe('AuthCallbackScreen: web auth completion (#2924)', () => {
  it('verifies a recovery link and lands on /reset-password', async () => {
    const router = openPage('/auth/recovery?token_hash=ok-1&type=recovery');

    await eventually(() => expect(lastPath(router)).toBe('/reset-password'));
    expect(verifyOtp).toHaveBeenCalledWith({ type: 'recovery', token_hash: 'ok-1' });
    expect(shows("didn't work")).toBe(false);
  });

  it('completes a confirm link and lands signed in on the library', async () => {
    const router = openPage('/auth/confirm?token_hash=ok-2&type=signup');

    await eventually(() => expect(lastPath(router)).toBe('/library'));
    expect(verifyOtp).toHaveBeenCalledWith({ type: 'signup', token_hash: 'ok-2' });
  });

  it('completes an OAuth callback and lands on the library', async () => {
    const router = openPage('/auth/callback?code=abc123');

    await eventually(() => expect(lastPath(router)).toBe('/library'));
    expect(exchangeCodeForSession).toHaveBeenCalledWith('abc123');
  });

  it('lands on the library for a second delivery of an already spent OAuth code, same as a fresh success', async () => {
    const first = openPage('/auth/callback?code=abc123');
    await eventually(() => expect(lastPath(first)).toBe('/library'));
    first.unmount();

    const second = openPage('/auth/callback?code=abc123');

    await eventually(() => expect(lastPath(second)).toBe('/library'));
    expect(shows("didn't work")).toBe(false);
    expect(exchangeCodeForSession).toHaveBeenCalledTimes(1);
  });

  it('strips the code and tokens from window.location before completing', async () => {
    const router = openPage('/auth/callback?code=abc123');

    await eventually(() => expect(lastPath(router)).toBe('/library'));
    expect(addressBar.writes[0]).toBe(CALLBACK_PATH);
  });

  it('shows an error with a way back to sign-in for a failed exchange, and does not navigate to the library', async () => {
    verifyOtp.mockResolvedValue(rejectedBy('otp_expired', FORBIDDEN));
    const router = openPage('/auth/recovery?token_hash=expired&type=recovery');

    await eventually(() => expect(shows("didn't work")).toBe(true));
    expect(lastPath(router)).toBe('/auth/recovery');
    expect(shows("didn't work")).toBe(true);
    expect(shows('already been used')).toBe(true);
    expect(shows('Back to sign in')).toBe(true);
  });

  it('shows the error notice rather than an unhandled rejection when the exchange throws', async () => {
    exchangeCodeForSession.mockRejectedValue(new Error('transport blew up'));
    openPage('/auth/callback?code=bad');

    await eventually(() => expect(shows("didn't work")).toBe(true));
  });

  it('shows the pending state, not the error notice, while the exchange is still in flight', async () => {
    const release = holdExchange();
    const router = openPage('/auth/callback?code=abc123');

    expect(shows('Signing you in')).toBe(true);
    expect(shows("didn't work")).toBe(false);
    await eventually(() => expect(exchangeCodeForSession).toHaveBeenCalledWith('abc123'));

    release(SIGNED_IN);
    await eventually(() => expect(lastPath(router)).toBe('/library'));
  });

  it('still reports a refusal that arrives after unmount, without touching the unmounted screen', async () => {
    const release = holdExchange();
    const { unmount } = openPage('/auth/callback?code=abc123');
    await eventually(() => expect(exchangeCodeForSession).toHaveBeenCalledWith('abc123'));

    unmount();
    release(rejectedBy('flow_state_expired', BAD_REQUEST));
    await advance(TURN_MS);

    expect(logs.warnings).toHaveLength(1);
  });
});

describe('AuthCallbackScreen: what a caller can hand the page beyond the happy path (#2924 probe)', () => {
  it('strips the OAuth code from the address bar before landing on the library', async () => {
    const router = openPage('/auth/callback?code=code-secret-1');

    await eventually(() => expect(lastPath(router)).toBe('/library'));
    expect(exchangeCodeForSession).toHaveBeenCalledWith('code-secret-1');
    expect(addressBar.writes[0]).not.toContain('code-secret-1');
  });

  it('strips the recovery token_hash from the address bar before landing on /reset-password', async () => {
    const router = openPage('/auth/recovery?token_hash=hash-secret-2&type=recovery');

    await eventually(() => expect(lastPath(router)).toBe('/reset-password'));
    expect(addressBar.writes[0]).not.toContain('hash-secret-2');
  });

  it('strips the code from the address bar even when the exchange is rejected', async () => {
    exchangeCodeForSession.mockResolvedValue(rejectedBy('flow_state_expired', BAD_REQUEST));
    openPage('/auth/callback?code=code-secret-3');

    await eventually(() => expect(shows("didn't work")).toBe(true));
    expect(addressBar.writes[0]).not.toContain('code-secret-3');
  });

  it('strips an access/refresh token pair carried in the hash fragment', async () => {
    openPage('/auth/callback#access_token=at-secret-4&refresh_token=rt-secret-4&token_type=bearer');

    await eventually(() => expect(shows("didn't work")).toBe(true));
    const written = String(addressBar.writes[0]);
    expect(written).not.toContain('at-secret-4');
    expect(written).not.toContain('rt-secret-4');
  });

  it('strips a token_hash carried in the hash fragment of a confirm link', async () => {
    openPage('/auth/confirm?type=signup#token_hash=hash-secret-5');

    await eventually(() => expect(addressBar.writes.length).toBeGreaterThan(0));
    expect(addressBar.writes[0]).not.toContain('hash-secret-5');
  });

  it('shows the Supabase error redirect as a failure with the way back to sign-in, spending nothing', async () => {
    const router = openPage(
      '/auth/confirm?error=access_denied&error_code=otp_expired&error_description=Email+link+is+invalid+or+has+expired',
    );

    await eventually(() => expect(shows("didn't work")).toBe(true));
    expect(shows('Back to sign in')).toBe(true);
    expect(verifyOtp).not.toHaveBeenCalled();
    expect(exchangeCodeForSession).not.toHaveBeenCalled();
    expect(lastPath(router)).toBe('/auth/confirm');
  });

  it('shows the Supabase OAuth error redirect as a failure with the way back to sign-in', async () => {
    const router = openPage(
      '/auth/callback?error=access_denied&error_description=User+denied+access#error=access_denied&error_description=User+denied+access',
    );

    await eventually(() => expect(shows("didn't work")).toBe(true));
    expect(shows('Back to sign in')).toBe(true);
    expect(lastPath(router)).toBe('/auth/callback');
  });

  const CREDENTIALLESS_PATHS = [
    '/auth/callback',
    '/auth/confirm',
    '/auth/recovery',
    '/auth/callback?code=',
    '/auth/recovery?token_hash=&type=recovery',
  ];

  for (const path of CREDENTIALLESS_PATHS) {
    it(`shows the error with the way back to sign-in for ${path}, which carries no credential`, async () => {
      openPage(path);

      await eventually(() => expect(shows("didn't work")).toBe(true));
      expect(shows('Back to sign in')).toBe(true);
      expect(exchangeCodeForSession).not.toHaveBeenCalled();
      expect(verifyOtp).not.toHaveBeenCalled();
    });
  }

  it('settles out of the pending state on a code with broken percent-encoding', async () => {
    const router = openPage('/auth/callback?code=%E0%A4%A');

    await eventually(() => expect(shows('Signing you in')).toBe(false));
    expect(lastPath(router)).not.toBe('/reset-password');
  });

  it('spends the code once and lands on the library under StrictMode double-mount', async () => {
    window.history.pushState({}, '', '/auth/callback?code=code-strict-6');
    const router = renderRouter(ROUTES, {
      initialUrl: '/auth/callback?code=code-strict-6',
      wrapper: StrictPorts,
    });

    await eventually(() => expect(lastPath(router)).toBe('/library'));
    expect(exchangeCodeForSession).toHaveBeenCalledTimes(1);
    expect(shows("didn't work")).toBe(false);
  });

  it('spends the code once when the screen re-renders mid-exchange', async () => {
    window.history.pushState({}, '', '/auth/callback?code=code-rerender-7');
    const router = renderRouter(
      { ...ROUTES, 'auth/callback': RerenderingCallback },
      { initialUrl: '/auth/callback?code=code-rerender-7', wrapper: Ports },
    );

    act(() => rerenders.bump());
    act(() => rerenders.bump());

    await eventually(() => expect(lastPath(router)).toBe('/library'));
    expect(exchangeCodeForSession).toHaveBeenCalledTimes(1);
  });
});

describe('AuthCallbackScreen: web failures are reported with their cause', () => {
  const path = '/auth/callback?code=code-secret-9';

  it('warns with the cause of a refused exchange, without the url or code', async () => {
    exchangeCodeForSession.mockResolvedValue(rejectedBy('flow_state_expired', BAD_REQUEST));
    openPage(path);

    await eventually(() => expect(shows("didn't work")).toBe(true));
    expect(logs.warnings).toEqual([
      [expect.any(String), expect.objectContaining({ cause: 'gotrue_rejected' })],
    ]);
    expect(logs.text()).not.toContain('code-secret-9');
  });

  it('warns and shows connection copy when the exchange times out', async () => {
    holdExchange();
    openPage(path);
    await advance(AUTH_ACTION_TIMEOUT_MS);

    await eventually(() => expect(shows("didn't work")).toBe(true));
    expect(logs.warnings).toHaveLength(1);
    expect(shows('connection')).toBe(true);
    expect(shows('expired')).toBe(false);
  });
});
