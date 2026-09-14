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
jest.mock('@shared/auth/useSession', () => ({
  useSession: () =>
    mockSessionStatus === 'signed-in'
      ? { status: 'signed-in', session: { user: { id: 'u1' } } }
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
    act(() => markRecoveryUnlocked());

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
    act(() => markRecoveryUnlocked(start));
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
