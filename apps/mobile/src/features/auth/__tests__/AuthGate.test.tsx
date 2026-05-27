/**
 * AuthGate — branches on useSession's SessionState (Slice 10, AC#6).
 *
 * Verifies three transitions:
 * - loading → renders the splash node (testID="auth-splash")
 * - signed-out → renders an expo-router Redirect to /sign-in
 * - signed-in → renders children
 */
/* eslint-disable @typescript-eslint/no-require-imports */
import { render } from '@testing-library/react-native';
import { Text } from 'react-native';

type SessionState =
  | { status: 'loading' }
  | { status: 'signed-in'; session: unknown }
  | { status: 'signed-out' };

let mockSessionState: SessionState = { status: 'loading' };
jest.mock('../hooks/useSession', () => ({
  useSession: () => mockSessionState,
}));

const mockRedirect = jest.fn((_props: { href: string }) => null);
jest.mock('expo-router', () => ({
  Redirect: (props: { href: string }) => mockRedirect(props),
}));

beforeEach(() => {
  mockRedirect.mockClear();
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

  it('redirects to /sign-in when signed-out', () => {
    mockSessionState = { status: 'signed-out' };
    const { AuthGate } = require('../ui/AuthGate');
    render(
      <AuthGate>
        <Text testID="protected">Protected</Text>
      </AuthGate>,
    );
    expect(mockRedirect).toHaveBeenCalledWith({ href: '/sign-in' });
  });

  it('renders children when signed-in', () => {
    mockSessionState = { status: 'signed-in', session: { access_token: 'abc' } };
    const { AuthGate } = require('../ui/AuthGate');
    const { getByTestId } = render(
      <AuthGate>
        <Text testID="protected">Protected</Text>
      </AuthGate>,
    );
    expect(getByTestId('protected')).toBeTruthy();
    expect(mockRedirect).not.toHaveBeenCalled();
  });
});
