import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react-native';
import { StyleSheet } from 'react-native';

import type { SignOutResult } from '@shared/auth/useSignOut';
import { darkTheme, spacing } from '@shared/ui/theme';

import { AuthGate } from '@features/auth/ui/AuthGate';
import { InvalidRecoveryLinkNotice, SessionExpiredNotice } from '@features/auth/ui/AuthNotices';

jest.mock('expo-router', () => ({
  useSegments: () => [],
  useRouter: () => ({ replace: jest.fn(), push: jest.fn(), back: jest.fn() }),
  Redirect: () => null,
}));

jest.mock('@shared/auth/useSession', () => ({
  useSession: () => ({ status: 'loading' }),
}));

jest.mock('@shared/auth/sessionExpired', () => ({
  useSessionExpired: () => false,
}));

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { signOut: jest.fn().mockResolvedValue({ error: null }) } },
}));

const mockSignOut = jest.fn();
let mockState: SignOutResult = { status: 'idle' };

jest.mock('@shared/auth/useSignOut', () => ({
  useSignOut: () => ({ state: mockState, signOut: mockSignOut }),
}));

const ERROR_COPY = "Couldn't reach the server. You've been signed out on this device.";

beforeEach(() => {
  mockSignOut.mockReset();
  mockState = { status: 'idle' };
});

const centered = {
  flex: 1,
  alignItems: 'center',
  justifyContent: 'center',
  gap: spacing.md,
  backgroundColor: darkTheme.color.canvas,
};

function noticeStyle(testID: string) {
  return StyleSheet.flatten(screen.getByTestId(testID).props.style);
}

describe('the auth notice screens share one full-screen centered layout', () => {
  it('centers the splash on the canvas with no padding', () => {
    render(
      <AuthGate>
        <></>
      </AuthGate>,
    );

    expect(noticeStyle('auth-splash')).toEqual(centered);
  });

  it('centers the invalid-recovery-link notice and pads it', () => {
    render(<InvalidRecoveryLinkNotice />);

    expect(noticeStyle('invalid-recovery-link')).toEqual({ ...centered, padding: spacing.lg });
  });

  it('centers the session-expired notice and pads it', () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <SessionExpiredNotice />
      </QueryClientProvider>,
    );

    expect(noticeStyle('session-expired')).toEqual({ ...centered, padding: spacing.lg });
  });
});

describe('SessionExpiredNotice', () => {
  it('shows the error copy under the button when sign-out failed', () => {
    mockState = { status: 'error', error: new Error('offline') };

    render(<SessionExpiredNotice />);

    expect(screen.getByText(ERROR_COPY)).toBeTruthy();
  });

  it('shows no error copy before a sign-out fails', () => {
    render(<SessionExpiredNotice />);

    expect(screen.queryByText(ERROR_COPY)).toBeNull();
  });

  it('disables the button while signing out', () => {
    mockState = { status: 'loading' };

    render(<SessionExpiredNotice />);
    fireEvent.press(screen.getByTestId('session-expired-signin'));

    expect(screen.getByText('Signing out…')).toBeTruthy();
    expect(mockSignOut).not.toHaveBeenCalled();
  });

  it('signs out when the enabled button is pressed', () => {
    render(<SessionExpiredNotice />);

    fireEvent.press(screen.getByTestId('session-expired-signin'));

    expect(mockSignOut).toHaveBeenCalledTimes(1);
  });
});
