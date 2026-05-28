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

beforeEach(() => {
  mockRedirect.mockClear();
  mockSegments = [];
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
});
