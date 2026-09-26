// Regression for issue #656: a bare `altune://reset-password` deep link used to
// reach the "choose a new password" form for a signed-in user with no recovery
// token ever verified, letting them change the real account password. AuthGate
// must render that route only after a recovery exchange has been unlocked.
import { render, screen, act } from '@testing-library/react-native';
import { Text } from 'react-native';

import { AuthGate } from '../ui/AuthGate';
import {
  clearRecoveryUnlock,
  markRecoveryUnlocked,
  RECOVERY_UNLOCK_WINDOW_MS,
} from '../recoveryUnlock';

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

let mockSessionStatus: 'loading' | 'signed-in' | 'signed-out' = 'signed-in';
let mockSignedInUserId = 'user-a';
jest.mock('@shared/auth/useSession', () => ({
  useSession: () =>
    mockSessionStatus === 'signed-in'
      ? { status: 'signed-in', session: { user: { id: mockSignedInUserId } } }
      : { status: mockSessionStatus },
}));

jest.mock('@shared/auth/sessionExpired', () => ({
  useSessionExpired: () => false,
}));

// AuthGate transitively imports the real Supabase client (via
// SessionExpiredNotice -> useSignOut), which cannot initialize its realtime
// socket under Node. Stub it; the recovery guard never touches Supabase.
jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { signOut: jest.fn().mockResolvedValue({ error: null }) } },
}));

function Children() {
  return <Text testID="reset-password-form">choose a new password</Text>;
}

beforeEach(() => {
  mockSegments = ['reset-password'];
  mockSessionStatus = 'signed-in';
  mockSignedInUserId = 'user-a';
  mockReplace.mockReset();
  act(() => clearRecoveryUnlock());
});

afterEach(() => {
  act(() => clearRecoveryUnlock());
});

describe('AuthGate: reset-password route is gated by a verified recovery exchange (#656)', () => {
  it('shows the invalid-link notice, not the password form, for a bare deep link with no recovery exchange', () => {
    render(
      <AuthGate>
        <Children />
      </AuthGate>,
    );

    expect(screen.queryByTestId('reset-password-form')).toBeNull();
    expect(screen.getByTestId('invalid-recovery-link')).toBeTruthy();
  });

  it('renders the password form once a recovery exchange has unlocked it', () => {
    act(() => markRecoveryUnlocked('user-a'));

    render(
      <AuthGate>
        <Children />
      </AuthGate>,
    );

    expect(screen.getByTestId('reset-password-form')).toBeTruthy();
    expect(screen.queryByTestId('invalid-recovery-link')).toBeNull();
  });

  it('shows the invalid-link notice again once the unlock window has expired', () => {
    const start = 1_000_000;
    act(() => markRecoveryUnlocked('user-a', start));
    // Advance past the window: the marker was set relative to `start`, and the
    // component reads Date.now(), so freeze it beyond the deadline.
    jest.spyOn(Date, 'now').mockReturnValue(start + RECOVERY_UNLOCK_WINDOW_MS + 1);

    render(
      <AuthGate>
        <Children />
      </AuthGate>,
    );

    expect(screen.queryByTestId('reset-password-form')).toBeNull();
    expect(screen.getByTestId('invalid-recovery-link')).toBeTruthy();

    (Date.now as jest.Mock).mockRestore();
  });
});

// Regression for issue #1638: the window used to be a bare deadline with no
// owner, so a recovery started for one account and abandoned handed the "choose
// a new password" form to whatever account was active next on the same process —
// with no recovery token ever verified for that identity.
describe('AuthGate: the unlock belongs to the account it was verified for (#1638)', () => {
  it('shows the invalid-link notice when the active session is a different account from the one the recovery unlocked', () => {
    act(() => markRecoveryUnlocked('user-a'));
    mockSignedInUserId = 'user-b';

    render(
      <AuthGate>
        <Children />
      </AuthGate>,
    );

    expect(screen.queryByTestId('reset-password-form')).toBeNull();
    expect(screen.getByTestId('invalid-recovery-link')).toBeTruthy();
  });

  it('shows the invalid-link notice when the recovery was abandoned and nobody is signed in', () => {
    act(() => markRecoveryUnlocked('user-a'));
    mockSessionStatus = 'signed-out';

    render(
      <AuthGate>
        <Children />
      </AuthGate>,
    );

    expect(screen.queryByTestId('reset-password-form')).toBeNull();
    expect(screen.getByTestId('invalid-recovery-link')).toBeTruthy();
  });

  it('still renders the password form for the account the recovery was verified for', () => {
    act(() => markRecoveryUnlocked('user-b'));
    mockSignedInUserId = 'user-b';

    render(
      <AuthGate>
        <Children />
      </AuthGate>,
    );

    expect(screen.getByTestId('reset-password-form')).toBeTruthy();
    expect(screen.queryByTestId('invalid-recovery-link')).toBeNull();
  });
});

// Regression for issue #2924: the web auth-completion route needs to run for a
// signed-out visitor — that is the whole point of a confirm/recovery/OAuth
// link — so `/auth/*` must be exempt from the same-origin redirect to
// `/sign-in` the same way the `(auth)` group already is.
describe('AuthGate: /auth/* renders for a signed-out visitor (#2924)', () => {
  it('does not redirect a signed-out visitor on /auth/callback to /sign-in', () => {
    mockSegments = ['auth', 'callback'];
    mockSessionStatus = 'signed-out';

    render(
      <AuthGate>
        <Children />
      </AuthGate>,
    );

    expect(screen.queryByTestId('redirect')).toBeNull();
    expect(screen.getByTestId('reset-password-form')).toBeTruthy();
  });

  it('still redirects a signed-out visitor outside /auth/* and (auth) to /sign-in', () => {
    mockSegments = ['library'];
    mockSessionStatus = 'signed-out';

    render(
      <AuthGate>
        <Children />
      </AuthGate>,
    );

    expect(screen.getByTestId('redirect')).toHaveTextContent('/sign-in');
  });
});

describe('AuthGate: a signed-out visitor already inside the (auth) group is also exempt (#2924)', () => {
  it('does not redirect a signed-out visitor already inside the (auth) group', () => {
    mockSegments = ['(auth)', 'sign-in'];
    mockSessionStatus = 'signed-out';

    render(
      <AuthGate>
        <Children />
      </AuthGate>,
    );

    expect(screen.queryByTestId('redirect')).toBeNull();
    expect(screen.getByTestId('reset-password-form')).toBeTruthy();
  });

  it('never redirects a signed-in visitor outside the (auth) group, /auth/* aside', () => {
    mockSegments = ['library'];
    mockSessionStatus = 'signed-in';

    render(
      <AuthGate>
        <Children />
      </AuthGate>,
    );

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
  ])('still redirects a signed-out visitor on %j to /sign-in', (segments) => {
    mockSegments = segments;
    mockSessionStatus = 'signed-out';

    render(
      <AuthGate>
        <Children />
      </AuthGate>,
    );

    expect(screen.getByTestId('redirect')).toHaveTextContent('/sign-in');
    expect(screen.queryByTestId('reset-password-form')).toBeNull();
  });

  it.each([[['auth', 'confirm']], [['auth', 'recovery']]])(
    'does not redirect a signed-out visitor on %j to /sign-in',
    (segments) => {
      mockSegments = segments;
      mockSessionStatus = 'signed-out';

      render(
        <AuthGate>
          <Children />
        </AuthGate>,
      );

      expect(screen.queryByTestId('redirect')).toBeNull();
      expect(screen.getByTestId('reset-password-form')).toBeTruthy();
    },
  );

  it.each([[['auth', 'callback']], [['auth', 'recovery']]])(
    'lets an already signed-in visitor on %j reach the completion screen',
    (segments) => {
      mockSegments = segments;
      mockSessionStatus = 'signed-in';

      render(
        <AuthGate>
          <Children />
        </AuthGate>,
      );

      expect(screen.queryByTestId('redirect')).toBeNull();
      expect(screen.getByTestId('reset-password-form')).toBeTruthy();
    },
  );
});
