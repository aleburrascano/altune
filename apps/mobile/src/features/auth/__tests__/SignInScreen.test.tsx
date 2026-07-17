/**
 * SignInScreen — exercises the form + error surface (Slice 11, AC#2, AC#3).
 *
 * Mocks the supabase singleton's auth.signInWithPassword so the screen's
 * useSignIn dispatches into a controlled outcome. AC#3 specifically says
 * NO assertion on the wording of the error — just presence + non-empty
 * text at testID="auth-error".
 */
/* eslint-disable @typescript-eslint/no-require-imports */
import { fireEvent, render, waitFor } from '@testing-library/react-native';

const mockSignIn = jest.fn();
jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: {
    auth: {
      signInWithPassword: (args: unknown) => mockSignIn(args),
    },
  },
}));

beforeEach(() => {
  mockSignIn.mockReset();
});

describe('SignInScreen', () => {
  it('calls signInWithPassword with the form inputs', async () => {
    mockSignIn.mockResolvedValueOnce({ error: null });
    const { SignInScreen } = require('../ui/SignInScreen');
    const { getByTestId } = render(<SignInScreen />);

    fireEvent.changeText(getByTestId('email-input'), 'user@example.com');
    fireEvent.changeText(getByTestId('password-input'), 'hunter2');
    fireEvent.press(getByTestId('submit-button'));

    await waitFor(() =>
      expect(mockSignIn).toHaveBeenCalledWith({
        email: 'user@example.com',
        password: 'hunter2',
      }),
    );
  });

  it('renders auth-error testID with non-empty text on credentials failure', async () => {
    mockSignIn.mockResolvedValueOnce({ error: { message: 'Invalid login credentials' } });
    const { SignInScreen } = require('../ui/SignInScreen');
    const { getByTestId, queryByTestId } = render(<SignInScreen />);

    fireEvent.changeText(getByTestId('email-input'), 'wrong@example.com');
    fireEvent.changeText(getByTestId('password-input'), 'wrong');
    fireEvent.press(getByTestId('submit-button'));

    await waitFor(() => {
      expect(queryByTestId('auth-error')).not.toBeNull();
    });
    const errorNode = getByTestId('auth-error');
    expect(errorNode.props.children).toBeTruthy();
  });

  it('renders auth-error on network failure', async () => {
    mockSignIn.mockRejectedValueOnce(new Error('Network request failed'));
    const { SignInScreen } = require('../ui/SignInScreen');
    const { getByTestId, queryByTestId } = render(<SignInScreen />);

    fireEvent.changeText(getByTestId('email-input'), 'user@example.com');
    fireEvent.changeText(getByTestId('password-input'), 'pw');
    fireEvent.press(getByTestId('submit-button'));

    await waitFor(() => {
      expect(queryByTestId('auth-error')).not.toBeNull();
    });
  });

  it('surfaces a distinct, non-generic message for network failures (AC#9)', async () => {
    const { NETWORK_ERROR_COPY } = require('../lib/errorCopy');
    mockSignIn.mockRejectedValueOnce(new Error('Network request failed'));
    const { SignInScreen } = require('../ui/SignInScreen');
    const { getByTestId, findByText } = render(<SignInScreen />);

    fireEvent.changeText(getByTestId('email-input'), 'user@example.com');
    fireEvent.changeText(getByTestId('password-input'), 'pw');
    fireEvent.press(getByTestId('submit-button'));

    expect(await findByText(NETWORK_ERROR_COPY)).toBeTruthy();
  });

  it('does NOT render auth-error on success', async () => {
    mockSignIn.mockResolvedValueOnce({ error: null });
    const { SignInScreen } = require('../ui/SignInScreen');
    const { getByTestId, queryByTestId } = render(<SignInScreen />);

    fireEvent.changeText(getByTestId('email-input'), 'user@example.com');
    fireEvent.changeText(getByTestId('password-input'), 'pw');
    fireEvent.press(getByTestId('submit-button'));

    await waitFor(() => expect(mockSignIn).toHaveBeenCalled());
    expect(queryByTestId('auth-error')).toBeNull();
  });
});
