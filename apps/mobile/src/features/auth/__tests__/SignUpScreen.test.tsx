/**
 * SignUpScreen — exercises the form + error surface (Slice 12, AC#1).
 *
 * Mocks supabase.auth.signUp. AC#1's full happy path (Supabase actually
 * creating a row in auth.users) is exercised by the manual smoke; this
 * component test pins that submitting the form dispatches into the SDK
 * with the right args and the error testID surfaces correctly.
 */
/* eslint-disable @typescript-eslint/no-require-imports */
import { fireEvent, render, waitFor } from '@testing-library/react-native';

const mockSignUp = jest.fn();
jest.mock('../api/supabaseClient', () => ({
  supabase: {
    auth: {
      signUp: (args: unknown) => mockSignUp(args),
    },
  },
}));

beforeEach(() => {
  mockSignUp.mockReset();
});

describe('SignUpScreen', () => {
  it('calls signUp with the form inputs', async () => {
    mockSignUp.mockResolvedValueOnce({ error: null });
    const { SignUpScreen } = require('../ui/SignUpScreen');
    const { getByTestId } = render(<SignUpScreen />);

    fireEvent.changeText(getByTestId('email-input'), 'new@example.com');
    fireEvent.changeText(getByTestId('password-input'), 'hunter2hunter2');
    fireEvent.press(getByTestId('submit-button'));

    await waitFor(() =>
      expect(mockSignUp).toHaveBeenCalledWith({
        email: 'new@example.com',
        password: 'hunter2hunter2',
      }),
    );
  });

  it('renders auth-error testID with non-empty text on signup failure', async () => {
    mockSignUp.mockResolvedValueOnce({ error: { message: 'User already registered' } });
    const { SignUpScreen } = require('../ui/SignUpScreen');
    const { getByTestId, queryByTestId } = render(<SignUpScreen />);

    fireEvent.changeText(getByTestId('email-input'), 'dup@example.com');
    fireEvent.changeText(getByTestId('password-input'), 'pw');
    fireEvent.press(getByTestId('submit-button'));

    await waitFor(() => expect(queryByTestId('auth-error')).not.toBeNull());
    expect(getByTestId('auth-error').props.children).toBeTruthy();
  });

  it('does NOT render auth-error on success', async () => {
    mockSignUp.mockResolvedValueOnce({ error: null });
    const { SignUpScreen } = require('../ui/SignUpScreen');
    const { getByTestId, queryByTestId } = render(<SignUpScreen />);

    fireEvent.changeText(getByTestId('email-input'), 'ok@example.com');
    fireEvent.changeText(getByTestId('password-input'), 'pw');
    fireEvent.press(getByTestId('submit-button'));

    await waitFor(() => expect(mockSignUp).toHaveBeenCalled());
    expect(queryByTestId('auth-error')).toBeNull();
  });
});
