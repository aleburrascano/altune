import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { SignInScreen } from '@features/auth/ui/SignInScreen';

import {
  authPortWrapper,
  createAuthPortMock,
  createSupabaseAuthMock,
} from '@features/auth/__tests__/testUtils/authTestUtils';

jest.mock('expo-router', () => {
  const { View } = require('react-native');
  return {
    useRouter: () => ({ replace: jest.fn(), push: jest.fn(), back: jest.fn() }),
    Link: ({ children, testID }: { children: ReactNode; testID?: string }) => (
      <View testID={testID}>{children}</View>
    ),
  };
});

const { signInWithPassword } = createSupabaseAuthMock('signInWithPassword');

const authPort = createAuthPortMock();

beforeEach(() => {
  authPort.reportSignInFailure.mockClear();
});

function renderSignIn(): void {
  render(<SignInScreen />, { wrapper: authPortWrapper(authPort) });
}

function rejectedWith(code: string) {
  return {
    data: { user: null, session: null },
    error: { name: 'AuthApiError', status: 400, code, message: 'rejected' },
  };
}

function submitAs(email: string, password: string): void {
  type('email-input', email);
  type('password-input', password);
  fireEvent.press(screen.getByTestId('submit-button'));
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

  it('submits the email without the whitespace around it', async () => {
    signInWithPassword.mockResolvedValue({ data: { user: {}, session: {} }, error: null });
    renderSignIn();

    submitAs('  ada@altune.app  ', 'hunter2!');

    await waitFor(() =>
      expect(signInWithPassword).toHaveBeenCalledWith({
        email: 'ada@altune.app',
        password: 'hunter2!',
      }),
    );
    expect(signInWithPassword).toHaveBeenCalledTimes(1);
  });

  it('shows the sign-in copy in the shared banner when the attempt failed', async () => {
    signInWithPassword.mockResolvedValue(rejectedWith('invalid_credentials'));
    renderSignIn();

    submitAs('ada@altune.app', 'hunter2!');

    expect(await screen.findByTestId('auth-error')).toHaveTextContent(
      'Email or password is incorrect.',
    );
  });

  it('blames nothing in particular when the failure has no known reason', async () => {
    signInWithPassword.mockResolvedValue(rejectedWith('some_future_code'));
    renderSignIn();

    submitAs('ada@altune.app', 'hunter2!');

    expect(await screen.findByTestId('auth-error')).toHaveTextContent(
      "Couldn't sign you in. Please try again.",
    );
  });

  it('shows no banner while no attempt has failed', () => {
    renderSignIn();

    expect(screen.queryByTestId('auth-error')).toBeNull();
  });
});
