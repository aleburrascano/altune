import { fireEvent, screen } from '@testing-library/react-native';
import { renderRouter } from 'expo-router/testing-library';
import { SetNewPasswordScreen } from '@features/signIn/view/SetNewPasswordScreen';
import { SignUpScreen } from '@features/signIn/view/SignUpScreen';
import { authPortWrapper } from '@features/signIn/__tests__/authTestUtils';
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
    renderRouter({ index: SignUpScreen }, { wrapper: authPortWrapper() });

    expect(inputProps('password-input')).toEqual({
      ...offersAGeneratedPassword,
      placeholder: 'Password',
      value: '',
    });
  });

  it('offers a generated password on the sign-up confirm field', () => {
    renderRouter({ index: SignUpScreen }, { wrapper: authPortWrapper() });

    expect(inputProps('confirm-input')).toEqual({
      ...offersAGeneratedPassword,
      placeholder: 'Confirm password',
      value: '',
    });
  });

  it('offers a generated password on the reset screen password field', () => {
    renderRouter({ index: SetNewPasswordScreen }, { wrapper: authPortWrapper() });

    expect(inputProps('password-input')).toEqual({
      ...offersAGeneratedPassword,
      placeholder: 'New password',
      value: '',
    });
  });

  it('offers a generated password on the reset screen confirm field', () => {
    renderRouter({ index: SetNewPasswordScreen }, { wrapper: authPortWrapper() });

    expect(inputProps('confirm-input')).toEqual({
      ...offersAGeneratedPassword,
      placeholder: 'Confirm new password',
      value: '',
    });
  });

  it('reports what was typed back to the screen holding the value', () => {
    renderRouter({ index: SetNewPasswordScreen }, { wrapper: authPortWrapper() });

    fireEvent.changeText(screen.getByTestId('password-input'), 'correct horse battery');

    expect(screen.getByTestId('password-input').props.value).toBe('correct horse battery');
  });
});
