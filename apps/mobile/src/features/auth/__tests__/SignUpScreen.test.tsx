// Pins what each credential screen renders now that AuthForm's showConfirm /
// enforcePasswordPolicy / showForgotPassword flags are gone (issue #1631). The
// difference between the two screens is composition, so a screen that composes
// the wrong pieces — or a shared piece that stops honouring one of them — fails
// here rather than in a store review.

import { fireEvent, render, screen } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { useOAuth } from '../hooks/useOAuth';
import { useSignUp } from '../hooks/useSignUp';
import { SignUpScreen } from '../ui/SignUpScreen';
import { PASSWORD_REQUIREMENTS_HINT } from '../validation';

jest.mock('expo-router', () => {
  const { View } = require('react-native');
  return {
    useRouter: () => ({ replace: jest.fn(), push: jest.fn(), back: jest.fn() }),
    Link: ({ children, testID }: { children: ReactNode; testID?: string }) => (
      <View testID={testID}>{children}</View>
    ),
  };
});

jest.mock('../hooks/useSignUp', () => ({ useSignUp: jest.fn() }));

jest.mock('../hooks/useOAuth', () => ({ useOAuth: jest.fn() }));

const signUp = jest.fn();

beforeEach(() => {
  jest.clearAllMocks();
  (useSignUp as jest.Mock).mockReturnValue({ state: { kind: 'idle' }, signUp });
  (useOAuth as jest.Mock).mockReturnValue({ state: { kind: 'idle' }, signInWith: jest.fn() });
});

function renderSignUp(state: unknown = { kind: 'idle' }): void {
  (useSignUp as jest.Mock).mockReturnValue({ state, signUp });
  render(<SignUpScreen />);
}

function type(testID: string, text: string): void {
  fireEvent.changeText(screen.getByTestId(testID), text);
}

function submitIsEnabled(): boolean {
  return screen.getByTestId('submit-button').props.accessibilityState.disabled === false;
}

describe('the sign-up screen', () => {
  it('asks the OS to generate a password for both new-password fields', () => {
    renderSignUp();

    const password = screen.getByTestId('password-input').props;
    const confirm = screen.getByTestId('confirm-input').props;
    expect([password.autoComplete, confirm.autoComplete]).toEqual(['new-password', 'new-password']);
  });

  it('offers no forgot-password link, having no account to recover yet', () => {
    renderSignUp();

    expect(screen.queryByTestId('link-to-forgot-password')).toBeNull();
  });

  it('names the password requirements once a weak password is typed', () => {
    renderSignUp();

    type('password-input', 'short');

    expect(screen.getByTestId('password-error')).toHaveTextContent(PASSWORD_REQUIREMENTS_HINT);
  });

  it('flags a confirmation that does not match', () => {
    renderSignUp();

    type('password-input', 'Hunter2!pass');
    type('confirm-input', 'Hunter2!pas');

    expect(screen.getByTestId('confirm-error')).toBeTruthy();
  });

  it('refuses to submit a password that fails the policy', () => {
    renderSignUp();

    type('email-input', 'ada@altune.app');
    type('password-input', 'short');
    type('confirm-input', 'short');

    expect(submitIsEnabled()).toBe(false);
  });

  it('refuses to submit until the confirmation matches', () => {
    renderSignUp();

    type('email-input', 'ada@altune.app');
    type('password-input', 'Hunter2!pass');

    expect(submitIsEnabled()).toBe(false);
  });

  it('submits once the email parses and the two strong passwords agree', () => {
    renderSignUp();

    type('email-input', 'ada@altune.app');
    type('password-input', 'Hunter2!pass');
    type('confirm-input', 'Hunter2!pass');
    fireEvent.press(screen.getByTestId('submit-button'));

    expect(signUp).toHaveBeenCalledWith('ada@altune.app', 'Hunter2!pass');
  });

  it('shows the sign-up copy in the shared banner when the attempt failed', () => {
    renderSignUp({ kind: 'error', reason: 'unknown' });

    expect(screen.getByTestId('auth-error')).toHaveTextContent(
      "Couldn't create your account. Please try again.",
    );
  });

  it('replaces the form with the check-email notice once the account is pending', () => {
    renderSignUp({ kind: 'awaiting-confirmation' });

    expect(screen.getByTestId('check-email-screen')).toBeTruthy();
    expect(screen.queryByTestId('sign-up-screen')).toBeNull();
  });
});
