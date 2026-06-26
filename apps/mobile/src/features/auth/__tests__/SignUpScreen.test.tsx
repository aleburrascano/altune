/**
 * SignUpScreen — form + validation + error surface.
 *
 * Updated for the auth-hardening spec (docs/specs/auth-hardening/spec.md):
 * sign-up now has a confirm-password field (AC#1), client-side email format
 * (AC#2) and password policy (AC#3) gating submit before any network call.
 * The anti-enumeration error-surface assertions (AC#5) are preserved.
 *
 * Mocks supabase.auth.signUp. A policy-valid password + matching confirm is
 * the precondition for the SDK to be reached at all.
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

const VALID_PASSWORD = 'Hunter2hunter!';

function fillValidForm(
  getByTestId: (id: string) => { props: Record<string, unknown> } & object,
  email = 'new@example.com',
) {
  fireEvent.changeText(getByTestId('email-input') as never, email);
  fireEvent.changeText(getByTestId('password-input') as never, VALID_PASSWORD);
  fireEvent.changeText(getByTestId('confirm-input') as never, VALID_PASSWORD);
}

describe('SignUpScreen', () => {
  it('calls signUp with the form inputs and a confirmation redirect once valid', async () => {
    mockSignUp.mockResolvedValueOnce({ data: { session: null }, error: null });
    const { SignUpScreen } = require('../ui/SignUpScreen');
    const { CONFIRM_REDIRECT_URL } = require('../hooks/useSignUp');
    const { getByTestId } = render(<SignUpScreen />);

    fillValidForm(getByTestId);
    fireEvent.press(getByTestId('submit-button'));

    await waitFor(() =>
      expect(mockSignUp).toHaveBeenCalledWith({
        email: 'new@example.com',
        password: VALID_PASSWORD,
        options: { emailRedirectTo: CONFIRM_REDIRECT_URL },
      }),
    );
  });

  it('shows the check-email state when sign-up returns no session (AC#4/AC#5)', async () => {
    mockSignUp.mockResolvedValueOnce({ data: { session: null }, error: null });
    const { SignUpScreen } = require('../ui/SignUpScreen');
    const { getByTestId, queryByTestId } = render(<SignUpScreen />);

    fillValidForm(getByTestId);
    fireEvent.press(getByTestId('submit-button'));

    await waitFor(() => expect(queryByTestId('check-email-screen')).not.toBeNull());
    expect(queryByTestId('auth-error')).toBeNull();
  });

  it('disables submit and shows a confirm error when passwords do not match (AC#1)', () => {
    const { SignUpScreen } = require('../ui/SignUpScreen');
    const { getByTestId, queryByTestId } = render(<SignUpScreen />);

    fireEvent.changeText(getByTestId('email-input'), 'new@example.com');
    fireEvent.changeText(getByTestId('password-input'), VALID_PASSWORD);
    fireEvent.changeText(getByTestId('confirm-input'), 'different123');

    expect(getByTestId('submit-button').props.accessibilityState.disabled).toBe(true);
    expect(queryByTestId('confirm-error')).not.toBeNull();

    fireEvent.press(getByTestId('submit-button'));
    expect(mockSignUp).not.toHaveBeenCalled();
  });

  it('enables submit once a mismatched confirm is corrected (AC#1)', () => {
    const { SignUpScreen } = require('../ui/SignUpScreen');
    const { getByTestId } = render(<SignUpScreen />);

    fireEvent.changeText(getByTestId('email-input'), 'new@example.com');
    fireEvent.changeText(getByTestId('password-input'), VALID_PASSWORD);
    fireEvent.changeText(getByTestId('confirm-input'), 'different123');
    expect(getByTestId('submit-button').props.accessibilityState.disabled).toBe(true);

    fireEvent.changeText(getByTestId('confirm-input'), VALID_PASSWORD);
    expect(getByTestId('submit-button').props.accessibilityState.disabled).toBe(false);
  });

  it('blocks submit on a too-short password before any network call (AC#3)', () => {
    const { SignUpScreen } = require('../ui/SignUpScreen');
    const { getByTestId, queryByTestId } = render(<SignUpScreen />);

    fireEvent.changeText(getByTestId('email-input'), 'new@example.com');
    fireEvent.changeText(getByTestId('password-input'), 'short');
    fireEvent.changeText(getByTestId('confirm-input'), 'short');

    expect(getByTestId('submit-button').props.accessibilityState.disabled).toBe(true);
    expect(queryByTestId('password-error')).not.toBeNull();
    fireEvent.press(getByTestId('submit-button'));
    expect(mockSignUp).not.toHaveBeenCalled();
  });

  it('blocks submit on a malformed email (AC#2)', () => {
    const { SignUpScreen } = require('../ui/SignUpScreen');
    const { getByTestId, queryByTestId } = render(<SignUpScreen />);

    fireEvent.changeText(getByTestId('email-input'), 'not-an-email');
    fireEvent.changeText(getByTestId('password-input'), VALID_PASSWORD);
    fireEvent.changeText(getByTestId('confirm-input'), VALID_PASSWORD);

    expect(getByTestId('submit-button').props.accessibilityState.disabled).toBe(true);
    expect(queryByTestId('email-error')).not.toBeNull();
  });

  it('renders auth-error testID with non-empty text on signup failure (AC#5)', async () => {
    mockSignUp.mockResolvedValueOnce({ error: { message: 'User already registered' } });
    const { SignUpScreen } = require('../ui/SignUpScreen');
    const { getByTestId, queryByTestId } = render(<SignUpScreen />);

    fillValidForm(getByTestId, 'dup@example.com');
    fireEvent.press(getByTestId('submit-button'));

    await waitFor(() => expect(queryByTestId('auth-error')).not.toBeNull());
    expect(getByTestId('auth-error').props.children).toBeTruthy();
  });

  it('does NOT render auth-error when a session is returned immediately', async () => {
    mockSignUp.mockResolvedValueOnce({ data: { session: { access_token: 'x' } }, error: null });
    const { SignUpScreen } = require('../ui/SignUpScreen');
    const { getByTestId, queryByTestId } = render(<SignUpScreen />);

    fillValidForm(getByTestId, 'ok@example.com');
    fireEvent.press(getByTestId('submit-button'));

    await waitFor(() => expect(mockSignUp).toHaveBeenCalled());
    expect(queryByTestId('auth-error')).toBeNull();
    expect(queryByTestId('check-email-screen')).toBeNull();
  });
});
