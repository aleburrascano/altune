import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { StyleSheet } from 'react-native';

import { darkTheme, spacing } from '@shared/ui/theme';

import { AuthGate } from '@features/auth/ui/AuthGate';
import { InvalidRecoveryLinkNotice, SessionExpiredNotice } from '@features/auth/ui/AuthNotices';

import { createSupabaseAuthMock } from '@features/auth/__tests__/testUtils/authTestUtils';

jest.mock('expo-router', () => ({
  useSegments: () => [],
  useRouter: () => ({ replace: jest.fn(), push: jest.fn(), back: jest.fn() }),
  Redirect: () => null,
}));

const { getSession, onAuthStateChange, signOut } = createSupabaseAuthMock(
  'getSession',
  'onAuthStateChange',
  'signOut',
);
const supabaseSignOut = signOut;

const ERROR_COPY = "Couldn't reach the server. You've been signed out on this device.";

beforeEach(() => {
  getSession.mockReturnValue(new Promise<never>(() => {}));
  onAuthStateChange.mockReturnValue({ data: { subscription: { unsubscribe: jest.fn() } } });
  supabaseSignOut.mockResolvedValue({ error: null });
});

type SignOutReply = { error: null };

function holdSignOut(): () => void {
  let release: ((reply: SignOutReply) => void) | null = null;
  supabaseSignOut.mockReturnValue(new Promise<SignOutReply>((resolve) => (release = resolve)));
  return () => release?.({ error: null });
}

function renderSessionExpired(): QueryClient {
  const queryClient = new QueryClient();
  queryClient.setQueryData(['library'], ['previous user track']);
  render(
    <QueryClientProvider client={queryClient}>
      <SessionExpiredNotice />
    </QueryClientProvider>,
  );
  return queryClient;
}

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
      <QueryClientProvider client={new QueryClient()}>
        <AuthGate>
          <></>
        </AuthGate>
      </QueryClientProvider>,
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
  it('shows the error copy under the button when sign-out failed', async () => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    supabaseSignOut.mockResolvedValue({ error: { name: 'AuthApiError', status: 500 } });
    renderSessionExpired();

    fireEvent.press(screen.getByTestId('session-expired-signin'));

    expect(await screen.findByText(ERROR_COPY)).toBeTruthy();
  });

  it('shows no error copy before a sign-out fails', () => {
    renderSessionExpired();

    expect(screen.queryByText(ERROR_COPY)).toBeNull();
  });

  it('disables the button while signing out', async () => {
    const finishSignOut = holdSignOut();
    renderSessionExpired();

    fireEvent.press(screen.getByTestId('session-expired-signin'));
    expect(await screen.findByText('Signing out…')).toBeTruthy();
    fireEvent.press(screen.getByTestId('session-expired-signin'));

    expect(supabaseSignOut).toHaveBeenCalledTimes(1);
    finishSignOut();
    expect(await screen.findByText('Sign in again')).toBeTruthy();
  });

  it('signs out and forgets the previous user cache when the enabled button is pressed', async () => {
    const queryClient = renderSessionExpired();

    fireEvent.press(screen.getByTestId('session-expired-signin'));

    await waitFor(() => expect(queryClient.getQueryData(['library'])).toBeUndefined());
    expect(supabaseSignOut).toHaveBeenCalledTimes(1);
    expect(screen.queryByText(ERROR_COPY)).toBeNull();
  });
});
