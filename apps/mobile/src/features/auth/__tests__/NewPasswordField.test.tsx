// Guards the autofill contract the new-password fields share (issue #1621; the
// sign-up password field joined them in #1631). They used to carry a copy each
// of the same four props; the props asserted here are what those copies
// produced, so a change to the shared component that stops a site offering a
// generated password fails.
import { fireEvent, render, screen } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { SetNewPasswordScreen } from '../ui/SetNewPasswordScreen';
import { SignUpScreen } from '../ui/SignUpScreen';

jest.mock('expo-router', () => ({
  useRouter: () => ({ replace: jest.fn(), push: jest.fn(), back: jest.fn() }),
  Link: ({ children }: { children: ReactNode }) => children,
}));

// Both screens reach the real Supabase client through their auth hooks, which
// cannot initialize its realtime socket under Node.
jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { updateUser: jest.fn(), signUp: jest.fn(), signInWithOAuth: jest.fn() } },
}));

const offersAGeneratedPassword = {
  secureTextEntry: true,
  autoCapitalize: 'none',
  textContentType: 'newPassword',
  autoComplete: 'new-password',
};

function inputProps(testID: string) {
  const { secureTextEntry, autoCapitalize, textContentType, autoComplete, placeholder, value } =
    screen.getByTestId(testID).props;
  return { secureTextEntry, autoCapitalize, textContentType, autoComplete, placeholder, value };
}

describe('the new-password fields share one autofill contract', () => {
  it('offers a generated password on the sign-up password field', () => {
    render(<SignUpScreen />);

    expect(inputProps('password-input')).toEqual({
      ...offersAGeneratedPassword,
      placeholder: 'Password',
      value: '',
    });
  });

  it('offers a generated password on the sign-up confirm field', () => {
    render(<SignUpScreen />);

    expect(inputProps('confirm-input')).toEqual({
      ...offersAGeneratedPassword,
      placeholder: 'Confirm password',
      value: '',
    });
  });

  it('offers a generated password on the reset screen password field', () => {
    render(<SetNewPasswordScreen />);

    expect(inputProps('password-input')).toEqual({
      ...offersAGeneratedPassword,
      placeholder: 'New password',
      value: '',
    });
  });

  it('offers a generated password on the reset screen confirm field', () => {
    render(<SetNewPasswordScreen />);

    expect(inputProps('confirm-input')).toEqual({
      ...offersAGeneratedPassword,
      placeholder: 'Confirm new password',
      value: '',
    });
  });

  it('reports what was typed back to the screen holding the value', () => {
    render(<SetNewPasswordScreen />);

    fireEvent.changeText(screen.getByTestId('password-input'), 'correct horse battery');

    expect(screen.getByTestId('password-input').props.value).toBe('correct horse battery');
  });
});
