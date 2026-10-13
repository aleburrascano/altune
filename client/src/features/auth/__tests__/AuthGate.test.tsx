import { render, screen, act } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Text } from 'react-native';

import { AuthGate } from '@features/auth/ui/AuthGate';
import { clearRecoveryUnlock, markRecoveryUnlocked } from '@features/auth/domain/passwordRecovery';

import { createSupabaseAuthMock } from '@features/auth/__tests__/testUtils/authTestUtils';

let mockSegments: string[] = [];
const mockReplace = jest.fn();

jest.mock('expo-router', () => ({
  useSegments: () => mockSegments,
  useRouter: () => ({ replace: mockReplace, push: jest.fn(), back: jest.fn() }),
  Redirect: ({ href }: { href: string }) => {
    const { Text: RNText } = require('react-native');
    return <RNText testID="redirect">{href}</RNText>;
  },
}));

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

async function renderGate(): Promise<void> {
  stubSupabaseAuth();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AuthGate>
        <Children />
      </AuthGate>
    </QueryClientProvider>,
  );
  await act(async () => {});
}

function Children() {
  return <Text testID="reset-password-form">choose a new password</Text>;
}

beforeEach(() => {
  mockSegments = ['reset-password'];
  sessionStatus = 'signed-in';
  signedInUserId = 'user-a';
  mockReplace.mockReset();
  act(() => clearRecoveryUnlock());
});

afterEach(() => {
  act(() => clearRecoveryUnlock());
});

const RECOVERY_UNLOCK_WINDOW_MS = 300_000;

describe('AuthGate: reset-password route is gated by a verified recovery exchange (#656)', () => {
  it('shows the invalid-link notice, not the password form, for a bare deep link with no recovery exchange', async () => {
    await renderGate();

    expect(screen.queryByTestId('reset-password-form')).toBeNull();
    expect(screen.getByTestId('invalid-recovery-link')).toBeTruthy();
  });

  it('renders the password form once a recovery exchange has unlocked it', async () => {
    act(() => markRecoveryUnlocked('user-a'));

    await renderGate();

    expect(screen.getByTestId('reset-password-form')).toBeTruthy();
    expect(screen.queryByTestId('invalid-recovery-link')).toBeNull();
  });

  it('shows the invalid-link notice again once the unlock window has expired', async () => {
    const start = 1_000_000;
    act(() => markRecoveryUnlocked('user-a', start));
    jest.spyOn(Date, 'now').mockReturnValue(start + RECOVERY_UNLOCK_WINDOW_MS + 1);

    await renderGate();

    expect(screen.queryByTestId('reset-password-form')).toBeNull();
    expect(screen.getByTestId('invalid-recovery-link')).toBeTruthy();

    (Date.now as jest.Mock).mockRestore();
  });
});

describe('AuthGate: the unlock belongs to the account it was verified for (#1638)', () => {
  it('shows the invalid-link notice when the active session is a different account from the one the recovery unlocked', async () => {
    act(() => markRecoveryUnlocked('user-a'));
    signedInUserId = 'user-b';

    await renderGate();

    expect(screen.queryByTestId('reset-password-form')).toBeNull();
    expect(screen.getByTestId('invalid-recovery-link')).toBeTruthy();
  });

  it('shows the invalid-link notice when the recovery was abandoned and nobody is signed in', async () => {
    act(() => markRecoveryUnlocked('user-a'));
    sessionStatus = 'signed-out';

    await renderGate();

    expect(screen.queryByTestId('reset-password-form')).toBeNull();
    expect(screen.getByTestId('invalid-recovery-link')).toBeTruthy();
  });

  it('still renders the password form for the account the recovery was verified for', async () => {
    act(() => markRecoveryUnlocked('user-b'));
    signedInUserId = 'user-b';

    await renderGate();

    expect(screen.getByTestId('reset-password-form')).toBeTruthy();
    expect(screen.queryByTestId('invalid-recovery-link')).toBeNull();
  });
});

describe('AuthGate: /auth/* renders for a signed-out visitor (#2924)', () => {
  it('does not redirect a signed-out visitor on /auth/callback to /sign-in', async () => {
    mockSegments = ['auth', 'callback'];
    sessionStatus = 'signed-out';

    await renderGate();

    expect(screen.queryByTestId('redirect')).toBeNull();
    expect(screen.getByTestId('reset-password-form')).toBeTruthy();
  });

  it('still redirects a signed-out visitor outside /auth/* and (auth) to /sign-in', async () => {
    mockSegments = ['library'];
    sessionStatus = 'signed-out';

    await renderGate();

    expect(screen.getByTestId('redirect')).toHaveTextContent('/sign-in');
  });
});

describe('AuthGate: a signed-out visitor already inside the (auth) group is also exempt (#2924)', () => {
  it('does not redirect a signed-out visitor already inside the (auth) group', async () => {
    mockSegments = ['(auth)', 'sign-in'];
    sessionStatus = 'signed-out';

    await renderGate();

    expect(screen.queryByTestId('redirect')).toBeNull();
    expect(screen.getByTestId('reset-password-form')).toBeTruthy();
  });

  it('never redirects a signed-in visitor outside the (auth) group, /auth/* aside', async () => {
    mockSegments = ['library'];
    sessionStatus = 'signed-in';

    await renderGate();

    expect(screen.queryByTestId('redirect')).toBeNull();
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
    mockSegments = segments;
    sessionStatus = 'signed-out';

    await renderGate();

    expect(screen.getByTestId('redirect')).toHaveTextContent('/sign-in');
    expect(screen.queryByTestId('reset-password-form')).toBeNull();
  });

  it.each([[['auth', 'confirm']], [['auth', 'recovery']]])(
    'does not redirect a signed-out visitor on %j to /sign-in',
    async (segments) => {
      mockSegments = segments;
      sessionStatus = 'signed-out';

      await renderGate();

      expect(screen.queryByTestId('redirect')).toBeNull();
      expect(screen.getByTestId('reset-password-form')).toBeTruthy();
    },
  );

  it.each([[['auth', 'callback']], [['auth', 'recovery']]])(
    'lets an already signed-in visitor on %j reach the completion screen',
    async (segments) => {
      mockSegments = segments;
      sessionStatus = 'signed-in';

      await renderGate();

      expect(screen.queryByTestId('redirect')).toBeNull();
      expect(screen.getByTestId('reset-password-form')).toBeTruthy();
    },
  );
});
