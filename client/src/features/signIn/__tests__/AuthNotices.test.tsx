import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, screen, waitFor } from '@testing-library/react-native';
import { renderRouter } from 'expo-router/testing-library';
import { StyleSheet } from 'react-native';

import { darkTheme, spacing } from '@shared/ui';
import { AuthGate, InvalidRecoveryLinkNotice, SessionExpiredNotice } from '@features/signIn';
import {
  authPortWrapper,
  createSupabaseAuthMock,
} from '@features/signIn/__tests__/testUtils/authTestUtils';
import { recordLogs } from '@testing/fakes/log/inMemoryLogger';

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
  renderRouter(
    { index: SessionExpiredNotice },
    {
      wrapper: ({ children }) => (
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      ),
    },
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

const Ports = authPortWrapper();

function noticeStyle(testID: string) {
  return StyleSheet.flatten(screen.getByTestId(testID).props.style);
}

describe('the auth notice screens share one full-screen centered layout', () => {
  it('centers the splash on the canvas with no padding', () => {
    renderRouter(
      {
        index: () => (
          <AuthGate>
            <></>
          </AuthGate>
        ),
      },
      {
        wrapper: ({ children }) => (
          <QueryClientProvider client={new QueryClient()}>
            <Ports>{children}</Ports>
          </QueryClientProvider>
        ),
      },
    );

    expect(noticeStyle('auth-splash')).toEqual(centered);
  });

  it('centers the invalid-recovery-link notice and pads it', () => {
    renderRouter({ index: InvalidRecoveryLinkNotice });

    expect(noticeStyle('invalid-recovery-link')).toEqual({ ...centered, padding: spacing.lg });
  });

  it('centers the session-expired notice and pads it', () => {
    renderRouter(
      { index: SessionExpiredNotice },
      {
        wrapper: ({ children }) => (
          <QueryClientProvider client={new QueryClient()}>
            <Ports>{children}</Ports>
          </QueryClientProvider>
        ),
      },
    );

    expect(noticeStyle('session-expired')).toEqual({ ...centered, padding: spacing.lg });
  });
});

describe('SessionExpiredNotice', () => {
  it('shows the error copy under the button when sign-out failed', async () => {
    recordLogs();
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
