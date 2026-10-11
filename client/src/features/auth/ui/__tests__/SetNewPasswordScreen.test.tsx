import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { Alert } from 'react-native';

import { SetNewPasswordScreen } from '@features/auth/ui/SetNewPasswordScreen';

import { createSupabaseAuthMock } from '@features/auth/__tests__/testUtils/authTestUtils';

const mockReplace = jest.fn();

jest.mock('expo-router', () => ({ useRouter: () => ({ replace: mockReplace }) }));

const { updateUser, signOut } = createSupabaseAuthMock('updateUser', 'signOut');

let alertSpy: jest.SpyInstance;

beforeEach(() => {
  mockReplace.mockReset();
  alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => undefined);
  updateUser.mockResolvedValue({ data: { user: {} }, error: null });
  signOut.mockResolvedValue({ error: null });
});

function submitNewPassword(): void {
  fireEvent.changeText(screen.getByTestId('password-input'), 'Hunter2!pass');
  fireEvent.changeText(screen.getByTestId('confirm-input'), 'Hunter2!pass');
  fireEvent.press(screen.getByTestId('submit-button'));
}

describe('SetNewPasswordScreen', () => {
  it('warns that other devices may still be signed in, then routes to the library', async () => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    signOut.mockResolvedValue({ error: { name: 'AuthApiError', status: 500, message: 'down' } });
    render(<SetNewPasswordScreen />);

    submitNewPassword();

    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith('/library'));
    expect(alertSpy).toHaveBeenCalledWith(
      expect.any(String),
      expect.stringContaining('may still be signed in'),
    );
  });

  it('routes without a warning when the other sessions were revoked', async () => {
    render(<SetNewPasswordScreen />);

    submitNewPassword();

    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith('/library'));
    expect(updateUser).toHaveBeenCalledWith({ password: 'Hunter2!pass' });
    expect(alertSpy).not.toHaveBeenCalled();
  });

  it('does not route before the update finishes', async () => {
    updateUser.mockReturnValue(new Promise(() => undefined));
    render(<SetNewPasswordScreen />);

    submitNewPassword();

    await waitFor(() => expect(updateUser).toHaveBeenCalledTimes(1));
    expect(mockReplace).not.toHaveBeenCalled();
  });
});
