import { screen, act } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Slot } from 'expo-router';
import { renderRouter } from 'expo-router/testing-library';
import type { ReactNode } from 'react';
import { Text } from 'react-native';

import { AuthGate, clearRecoveryUnlock, markRecoveryUnlocked } from '@features/signIn';
import {
  authPortWrapper,
  createSupabaseAuthMock,
} from '@features/signIn/__tests__/testUtils/authTestUtils';
import { freezeDate } from '@testing/freezeDate';

let sessionStatus: 'loading' | 'signed-in' | 'signed-out' = 'signed-in';
let signedInUserId = 'user-a';

const { getSession, onAuthStateChange } = createSupabaseAuthMock('getSession', 'onAuthStateChange');

function stubSupabaseAuth(): void {
  const session = sessionStatus === 'signed-in' ? { user: { id: signedInUserId } } : null;
  getSession.mockReturnValue(
    sessionStatus === 'loading'
      ? new Promise<never>(() => {})
      : Promise.resolve({ data: { session } }),
  );
  onAuthStateChange.mockReturnValue({ data: { subscription: { unsubscribe: jest.fn() } } });
}

function Layout() {
  return (
    <AuthGate>
      <Slot />
    </AuthGate>
  );
}

function Children() {
  return <Text testID="reset-password-form">choose a new password</Text>;
}

function SignInPage() {
  return <Text testID="sign-in-page">sign in</Text>;
}

const ROUTES = {
  _layout: Layout,
  'reset-password': Children,
  'auth/callback': Children,
  'auth/confirm': Children,
  'auth/recovery': Children,
  '(auth)/sign-in': SignInPage,
  library: Children,
  authx: Children,
  'auth-something': Children,
  'authentication/callback': Children,
  'library/auth/callback': Children,
};

const Ports = authPortWrapper();

interface WrapperProps {
  children: ReactNode;
}

function wrapper({ children }: WrapperProps) {
  return (
    <QueryClientProvider client={new QueryClient()}>
      <Ports>{children}</Ports>
    </QueryClientProvider>
  );
}

async function renderGate(url: string): Promise<ReturnType<typeof renderRouter>> {
  stubSupabaseAuth();
  const router = renderRouter(ROUTES, { initialUrl: url, wrapper });
  await act(async () => {});
  return router;
}

beforeEach(() => {
  sessionStatus = 'signed-in';
  signedInUserId = 'user-a';
  act(() => clearRecoveryUnlock());
});

afterEach(() => {
  jest.useRealTimers();
  act(() => clearRecoveryUnlock());
});

const RECOVERY_UNLOCK_WINDOW_MS = 300_000;

describe('AuthGate: reset-password route is gated by a verified recovery exchange (#656)', () => {
  it('shows the invalid-link notice, not the password form, for a bare deep link with no recovery exchange', async () => {
    await renderGate('/reset-password');

    expect(screen.queryByTestId('reset-password-form')).toBeNull();
    expect(screen.getByTestId('invalid-recovery-link')).toBeTruthy();
  });

  it('renders the password form once a recovery exchange has unlocked it', async () => {
    act(() => markRecoveryUnlocked('user-a'));

    await renderGate('/reset-password');

    expect(screen.getByTestId('reset-password-form')).toBeTruthy();
    expect(screen.queryByTestId('invalid-recovery-link')).toBeNull();
  });

  it('shows the invalid-link notice again once the unlock window has expired', async () => {
    const start = 1_000_000;
    act(() => markRecoveryUnlocked('user-a', start));
    freezeDate(start + RECOVERY_UNLOCK_WINDOW_MS + 1);

    await renderGate('/reset-password');

    expect(screen.queryByTestId('reset-password-form')).toBeNull();
    expect(screen.getByTestId('invalid-recovery-link')).toBeTruthy();
  });
});

describe('AuthGate: the unlock belongs to the account it was verified for (#1638)', () => {
  it('shows the invalid-link notice when the active session is a different account from the one the recovery unlocked', async () => {
    act(() => markRecoveryUnlocked('user-a'));
    signedInUserId = 'user-b';

    await renderGate('/reset-password');

    expect(screen.queryByTestId('reset-password-form')).toBeNull();
    expect(screen.getByTestId('invalid-recovery-link')).toBeTruthy();
  });

  it('shows the invalid-link notice when the recovery was abandoned and nobody is signed in', async () => {
    act(() => markRecoveryUnlocked('user-a'));
    sessionStatus = 'signed-out';

    await renderGate('/reset-password');

    expect(screen.queryByTestId('reset-password-form')).toBeNull();
    expect(screen.getByTestId('invalid-recovery-link')).toBeTruthy();
  });

  it('still renders the password form for the account the recovery was verified for', async () => {
    act(() => markRecoveryUnlocked('user-b'));
    signedInUserId = 'user-b';

    await renderGate('/reset-password');

    expect(screen.getByTestId('reset-password-form')).toBeTruthy();
    expect(screen.queryByTestId('invalid-recovery-link')).toBeNull();
  });
});

describe('AuthGate: /auth/* renders for a signed-out visitor (#2924)', () => {
  it('does not redirect a signed-out visitor on /auth/callback to /sign-in', async () => {
    sessionStatus = 'signed-out';

    const router = await renderGate('/auth/callback');

    expect(router.getPathname()).toBe('/auth/callback');
    expect(screen.getByTestId('reset-password-form')).toBeTruthy();
  });

  it('still redirects a signed-out visitor outside /auth/* and (auth) to /sign-in', async () => {
    sessionStatus = 'signed-out';

    const router = await renderGate('/library');

    expect(router.getPathname()).toBe('/sign-in');
    expect(screen.queryByTestId('reset-password-form')).toBeNull();
  });
});

describe('AuthGate: a signed-out visitor already inside the (auth) group is also exempt (#2924)', () => {
  it('does not redirect a signed-out visitor already inside the (auth) group', async () => {
    sessionStatus = 'signed-out';

    const router = await renderGate('/sign-in');

    expect(router.getPathname()).toBe('/sign-in');
    expect(screen.getByTestId('sign-in-page')).toBeTruthy();
  });

  it('never redirects a signed-in visitor outside the (auth) group, /auth/* aside', async () => {
    sessionStatus = 'signed-in';

    const router = await renderGate('/library');

    expect(router.getPathname()).toBe('/library');
    expect(screen.getByTestId('reset-password-form')).toBeTruthy();
  });
});

describe('AuthGate: the /auth/* exemption stops at the segment boundary (#2924 probe)', () => {
  it.each([
    [['authx']],
    [['auth-something']],
    [['authentication', 'callback']],
    [['library', 'auth', 'callback']],
  ])('still redirects a signed-out visitor on %j to /sign-in', async (segments) => {
    sessionStatus = 'signed-out';

    const router = await renderGate(`/${segments.join('/')}`);

    expect(router.getPathname()).toBe('/sign-in');
    expect(screen.queryByTestId('reset-password-form')).toBeNull();
  });

  it.each([[['auth', 'confirm']], [['auth', 'recovery']]])(
    'does not redirect a signed-out visitor on %j to /sign-in',
    async (segments) => {
      sessionStatus = 'signed-out';

      const router = await renderGate(`/${segments.join('/')}`);

      expect(router.getPathname()).toBe(`/${segments.join('/')}`);
      expect(screen.getByTestId('reset-password-form')).toBeTruthy();
    },
  );

  it.each([[['auth', 'callback']], [['auth', 'recovery']]])(
    'lets an already signed-in visitor on %j reach the completion screen',
    async (segments) => {
      sessionStatus = 'signed-in';

      const router = await renderGate(`/${segments.join('/')}`);

      expect(router.getPathname()).toBe(`/${segments.join('/')}`);
      expect(screen.getByTestId('reset-password-form')).toBeTruthy();
    },
  );
});
