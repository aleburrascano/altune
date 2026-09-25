// Pins what each credential screen renders now that AuthForm's showConfirm /
// enforcePasswordPolicy / showForgotPassword flags are gone (issue #1631). The
// difference between the two screens is composition, so a screen that composes
// the wrong pieces — or a shared piece that stops honouring one of them — fails
// here rather than in a store review.

import { fireEvent, render, screen } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { useOAuth } from '../hooks/useOAuth';
import { useSignIn } from '../hooks/useSignIn';
import { SignInScreen } from '../ui/SignInScreen';

jest.mock('expo-router', () => {
  const { View } = require('react-native');
  return {
    useRouter: () => ({ replace: jest.fn(), push: jest.fn(), back: jest.fn() }),
    Link: ({ children, testID }: { children: ReactNode; testID?: string }) => (
      <View testID={testID}>{children}</View>
    ),
  };
});

jest.mock('../hooks/useSignIn', () => ({ useSignIn: jest.fn() }));

jest.mock('../hooks/useOAuth', () => ({ useOAuth: jest.fn() }));

const signIn = jest.fn();

beforeEach(() => {
  jest.clearAllMocks();
  (useSignIn as jest.Mock).mockReturnValue({ state: { kind: 'idle' }, signIn });
  (useOAuth as jest.Mock).mockReturnValue({ state: { kind: 'idle' }, signInWith: jest.fn() });
});

function renderSignIn(state: unknown = { kind: 'idle' }): void {
  (useSignIn as jest.Mock).mockReturnValue({ state, signIn });
  render(<SignInScreen />);
}

function type(testID: string, text: string): void {
  fireEvent.changeText(screen.getByTestId(testID), text);
}

function submitIsEnabled(): boolean {
  return screen.getByTestId('submit-button').props.accessibilityState.disabled === false;
}

describe('the sign-in screen', () => {
  it('asks the OS for the saved password rather than a generated one', () => {
    renderSignIn();

    const { textContentType, autoComplete } = screen.getByTestId('password-input').props;
    expect({ textContentType, autoComplete }).toEqual({
      textContentType: 'password',
      autoComplete: 'password',
    });
  });

  it('offers no confirm field', () => {
    renderSignIn();

    expect(screen.queryByTestId('confirm-input')).toBeNull();
  });

  it('offers a way out to the forgot-password screen', () => {
    renderSignIn();

    expect(screen.getByTestId('link-to-forgot-password')).toBeTruthy();
  });

  it('never judges an existing password against the sign-up policy', () => {
    renderSignIn();

    type('password-input', 'short');

    expect(screen.queryByTestId('password-error')).toBeNull();
  });

  it('accepts any non-empty password once the email parses', () => {
    renderSignIn();

    type('email-input', 'ada@altune.app');
    type('password-input', 'short');

    expect(submitIsEnabled()).toBe(true);
  });

  it('refuses to submit while the email does not parse', () => {
    renderSignIn();

    type('email-input', 'ada@');
    type('password-input', 'hunter2!');

    expect(submitIsEnabled()).toBe(false);
  });

  it('submits the email without the whitespace around it', () => {
    renderSignIn();

    type('email-input', '  ada@altune.app  ');
    type('password-input', 'hunter2!');
    fireEvent.press(screen.getByTestId('submit-button'));

    expect(signIn).toHaveBeenCalledWith('ada@altune.app', 'hunter2!');
  });

  it('shows the sign-in copy in the shared banner when the attempt failed', () => {
    renderSignIn({ kind: 'error', reason: 'invalid_credentials' });

    expect(screen.getByTestId('auth-error')).toHaveTextContent('Email or password is incorrect.');
  });

  // A failure the hook could not name is not evidence about the password, and
  // the banner is where that mislabelling used to reach the user (#1646).
  it('blames nothing in particular when the failure has no known reason', () => {
    renderSignIn({ kind: 'error', reason: 'unknown' });

    expect(screen.getByTestId('auth-error')).toHaveTextContent(
      "Couldn't sign you in. Please try again.",
    );
  });

  it('shows no banner while no attempt has failed', () => {
    renderSignIn();

    expect(screen.queryByTestId('auth-error')).toBeNull();
  });
});
