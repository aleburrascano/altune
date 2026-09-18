// Guards the layout the three auth notice screens share (issue #1618). They
// used to carry three copies of the same inline style object; the computed
// style asserted here is what those copies produced, so a change to the shared
// component that alters any screen's appearance fails.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react-native';
import { StyleSheet } from 'react-native';

import { darkTheme, spacing } from '@shared/ui/theme';

import { AuthGate } from '../ui/AuthGate';
import { InvalidRecoveryLinkNotice } from '../ui/InvalidRecoveryLinkNotice';
import { SessionExpiredNotice } from '../ui/SessionExpiredNotice';

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

// The notices reach the real Supabase client through useSignOut, which cannot
// initialize its realtime socket under Node.
jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { signOut: jest.fn().mockResolvedValue({ error: null }) } },
}));

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
