/**
 * SetNewPasswordScreen — new password + confirm → updateUser, then route to
 * the library (AC#7). Validation (policy + match) gates submit. SDK + router
 * mocked.
 */
/* eslint-disable @typescript-eslint/no-require-imports */
import { fireEvent, render, waitFor } from '@testing-library/react-native';

const mockUpdateUser = jest.fn();
const mockReplace = jest.fn();
jest.mock('../api/supabaseClient', () => ({
  supabase: { auth: { updateUser: (a: unknown) => mockUpdateUser(a) } },
}));
jest.mock('expo-router', () => ({
  useRouter: () => ({ replace: mockReplace, push: jest.fn() }),
}));

beforeEach(() => {
  mockUpdateUser.mockReset();
  mockReplace.mockReset();
});

const VALID = 'Newpassword1!';

describe('SetNewPasswordScreen', () => {
  it('blocks submit on a mismatch and on a too-short password', () => {
    const { SetNewPasswordScreen } = require('../ui/SetNewPasswordScreen');
    const { getByTestId, queryByTestId } = render(<SetNewPasswordScreen />);

    fireEvent.changeText(getByTestId('password-input'), VALID);
    fireEvent.changeText(getByTestId('confirm-input'), 'different');
    expect(getByTestId('submit-button').props.accessibilityState.disabled).toBe(true);
    expect(queryByTestId('confirm-error')).not.toBeNull();

    fireEvent.changeText(getByTestId('password-input'), 'short');
    fireEvent.changeText(getByTestId('confirm-input'), 'short');
    expect(getByTestId('submit-button').props.accessibilityState.disabled).toBe(true);
    expect(queryByTestId('password-error')).not.toBeNull();
  });

  it('updates the password and routes to the library on success (AC#7)', async () => {
    mockUpdateUser.mockResolvedValueOnce({ data: {}, error: null });
    const { SetNewPasswordScreen } = require('../ui/SetNewPasswordScreen');
    const { getByTestId } = render(<SetNewPasswordScreen />);

    fireEvent.changeText(getByTestId('password-input'), VALID);
    fireEvent.changeText(getByTestId('confirm-input'), VALID);
    fireEvent.press(getByTestId('submit-button'));

    await waitFor(() => expect(mockUpdateUser).toHaveBeenCalledWith({ password: VALID }));
    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith('/library'));
  });

  it('surfaces an error banner when the update fails', async () => {
    mockUpdateUser.mockResolvedValueOnce({ data: {}, error: { message: 'nope' } });
    const { SetNewPasswordScreen } = require('../ui/SetNewPasswordScreen');
    const { getByTestId, queryByTestId } = render(<SetNewPasswordScreen />);

    fireEvent.changeText(getByTestId('password-input'), VALID);
    fireEvent.changeText(getByTestId('confirm-input'), VALID);
    fireEvent.press(getByTestId('submit-button'));

    await waitFor(() => expect(queryByTestId('auth-error')).not.toBeNull());
    expect(mockReplace).not.toHaveBeenCalled();
  });
});
