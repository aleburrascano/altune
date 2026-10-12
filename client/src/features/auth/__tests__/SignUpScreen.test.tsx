import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { CheckEmailNotice } from '@features/auth/ui/CheckEmailNotice';
import { SignUpScreen } from '@features/auth/ui/SignUpScreen';
import { PASSWORD_REQUIREMENTS_HINT } from '@features/auth/domain/validation';

import {
  authPortWrapper,
  createSupabaseAuthMock,
} from '@features/auth/__tests__/testUtils/authTestUtils';
import { warmUpFirstRender } from '@testing/warmUpFirstRender';

jest.mock('expo-router', () => {
  const { View } = require('react-native');
  return {
    useRouter: () => ({ replace: jest.fn(), push: jest.fn(), back: jest.fn() }),
    Link: ({ children, testID }: { children: ReactNode; testID?: string }) => (
      <View testID={testID}>{children}</View>
    ),
  };
});

const { signUp } = createSupabaseAuthMock('signUp');

warmUpFirstRender(async () => {
  render(<SignUpScreen />, { wrapper: authPortWrapper() });
  render(<CheckEmailNotice />);
});

function renderSignUp(): void {
  render(<SignUpScreen />, { wrapper: authPortWrapper() });
}

function rejectedWith(code: string) {
  return { data: null, error: { name: 'AuthApiError', status: 400, code, message: 'rejected' } };
}

function submitValidForm(): void {
  type('email-input', 'ada@altune.app');
  type('password-input', 'Hunter2!pass');
  type('confirm-input', 'Hunter2!pass');
  fireEvent.press(screen.getByTestId('submit-button'));
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

  it('submits once the email parses and the two strong passwords agree', async () => {
    signUp.mockResolvedValue({ data: { user: {}, session: {} }, error: null });
    renderSignUp();

    submitValidForm();

    await waitFor(() =>
      expect(signUp).toHaveBeenCalledWith(
        expect.objectContaining({ email: 'ada@altune.app', password: 'Hunter2!pass' }),
      ),
    );
    expect(signUp).toHaveBeenCalledTimes(1);
  });

  it('shows the sign-up copy in the shared banner when the attempt failed', async () => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    signUp.mockResolvedValue(rejectedWith('some_future_code'));
    renderSignUp();

    submitValidForm();

    expect(await screen.findByTestId('auth-error')).toHaveTextContent(
      "Couldn't create your account. Please try again.",
    );
  });

  it('tells the person sign-ups are closed when the server refused the sign-up', async () => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    signUp.mockResolvedValue(rejectedWith('signup_disabled'));
    renderSignUp();

    submitValidForm();

    expect(await screen.findByTestId('auth-error')).toHaveTextContent(
      'Sign-ups are closed on staging.',
    );
  });

  it('replaces the form with the check-email notice once the account is pending', async () => {
    signUp.mockResolvedValue({
      data: { user: { id: 'new-user', identities: [{ id: 'identity-1' }] }, session: null },
      error: null,
    });
    renderSignUp();

    submitValidForm();

    expect(await screen.findByTestId('check-email-screen')).toBeTruthy();
    expect(screen.queryByTestId('sign-up-screen')).toBeNull();
  });
});
