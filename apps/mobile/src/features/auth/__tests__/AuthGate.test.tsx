/**
 * AuthGate — branches on useSession's SessionState + the current route's
 * segment so it doesn't redirect signed-out users when they're already
 * inside the (auth) group (Slice 10, AC#6).
 */
/* eslint-disable @typescript-eslint/no-require-imports */
import { render } from '@testing-library/react-native';
import { Text } from 'react-native';

type SessionState =
  | { status: 'loading' }
  | { status: 'signed-in'; session: unknown }
  | { status: 'signed-out' };

let mockSessionState: SessionState = { status: 'loading' };
let mockSegments: string[] = [];
jest.mock('../hooks/useSession', () => ({
  useSession: () => mockSessionState,
}));

const mockRedirect = jest.fn((_props: { href: string }) => null);
jest.mock('expo-router', () => ({
  Redirect: (props: { href: string }) => mockRedirect(props),
  useSegments: () => mockSegments,
}));

// Stubbed so these stay routing tests: the real notice pulls in useSignOut ->
// the Supabase singleton. Its own behaviour is covered in
// SessionExpiredNotice.test.tsx.
jest.mock('../ui/SessionExpiredNotice', () => {
  const { Text } = require('react-native');
  return { SessionExpiredNotice: () => <Text testID="session-expired">expired</Text> };
});

const { clearSessionExpired, markSessionExpired } = require('@shared/auth/sessionExpired');

beforeEach(() => {
  mockRedirect.mockClear();
  mockSegments = [];
  clearSessionExpired();
});

describe('AuthGate', () => {
  it('renders splash while loading', () => {
    mockSessionState = { status: 'loading' };
    const { AuthGate } = require('../ui/AuthGate');
    const { getByTestId } = render(
      <AuthGate>
        <Text testID="protected">Protected</Text>
      </AuthGate>,
    );
    expect(getByTestId('auth-splash')).toBeTruthy();
  });

  it('redirects signed-out users to /sign-in when NOT already in (auth)', () => {
    mockSessionState = { status: 'signed-out' };
    mockSegments = ['(app)']; // any non-(auth) group
    const { AuthGate } = require('../ui/AuthGate');
    render(
      <AuthGate>
        <Text testID="protected">Protected</Text>
      </AuthGate>,
    );
    expect(mockRedirect).toHaveBeenCalledWith({ href: '/sign-in' });
  });

  it('does NOT redirect signed-out users who are already in the (auth) group', () => {
    mockSessionState = { status: 'signed-out' };
    mockSegments = ['(auth)', 'sign-in'];
    const { AuthGate } = require('../ui/AuthGate');
    const { getByTestId } = render(
      <AuthGate>
        <Text testID="auth-child">Auth Child</Text>
      </AuthGate>,
    );
    expect(mockRedirect).not.toHaveBeenCalled();
    expect(getByTestId('auth-child')).toBeTruthy();
  });

  it('redirects signed-in users out of (auth) group to /library', () => {
    mockSessionState = { status: 'signed-in', session: { access_token: 'abc' } };
    mockSegments = ['(auth)', 'sign-in'];
    const { AuthGate } = require('../ui/AuthGate');
    render(
      <AuthGate>
        <Text testID="auth-child">Auth Child</Text>
      </AuthGate>,
    );
    expect(mockRedirect).toHaveBeenCalledWith({ href: '/library' });
  });

  it('renders children for signed-in users outside (auth)', () => {
    mockSessionState = { status: 'signed-in', session: { access_token: 'abc' } };
    mockSegments = ['library'];
    const { AuthGate } = require('../ui/AuthGate');
    const { getByTestId } = render(
      <AuthGate>
        <Text testID="protected">Protected</Text>
      </AuthGate>,
    );
    expect(getByTestId('protected')).toBeTruthy();
    expect(mockRedirect).not.toHaveBeenCalled();
  });

  it('shows the expired notice when the backend rejected the token', () => {
    // The soft-lock: SDK says signed-in, backend says 401. Without this the
    // user sees a permanently-failing screen and no way to re-authenticate.
    mockSessionState = { status: 'signed-in', session: { access_token: 'abc' } };
    mockSegments = ['library'];
    markSessionExpired();
    const { AuthGate } = require('../ui/AuthGate');
    const { getByTestId, queryByTestId } = render(
      <AuthGate>
        <Text testID="protected">Protected</Text>
      </AuthGate>,
    );
    expect(getByTestId('session-expired')).toBeTruthy();
    expect(queryByTestId('protected')).toBeNull();
  });

  it('does not show the expired notice to a signed-out user', () => {
    // Signed-out already redirects to /sign-in; the notice would be redundant
    // and would swallow the redirect.
    mockSessionState = { status: 'signed-out' };
    mockSegments = ['(app)'];
    markSessionExpired();
    const { AuthGate } = require('../ui/AuthGate');
    const { queryByTestId } = render(
      <AuthGate>
        <Text testID="protected">Protected</Text>
      </AuthGate>,
    );
    expect(queryByTestId('session-expired')).toBeNull();
    expect(mockRedirect).toHaveBeenCalledWith({ href: '/sign-in' });
  });
});
