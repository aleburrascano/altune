/**
 * ForgotPasswordScreen — email → reset request. Anti-enumeration: always the
 * same "sent" state on a resolved response (AC#6). Mocks the SDK + router.
 */
/* eslint-disable @typescript-eslint/no-require-imports */
import { fireEvent, render, waitFor } from '@testing-library/react-native';

const mockReset = jest.fn();
jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { resetPasswordForEmail: (...a: unknown[]) => mockReset(...a) } },
}));
jest.mock('expo-router', () => {
  const { View } = require('react-native');
  return {
    useRouter: () => ({ replace: jest.fn(), push: jest.fn() }),
    Link: ({ children, testID }: { children: unknown; testID?: string }) => (
      <View testID={testID}>{children as never}</View>
    ),
  };
});

beforeEach(() => mockReset.mockReset());

describe('ForgotPasswordScreen', () => {
  it('disables submit until the email is valid (AC#2)', () => {
    const { ForgotPasswordScreen } = require('../ui/ForgotPasswordScreen');
    const { getByTestId } = render(<ForgotPasswordScreen />);
    expect(getByTestId('submit-button').props.accessibilityState.disabled).toBe(true);

    fireEvent.changeText(getByTestId('email-input'), 'me@example.com');
    expect(getByTestId('submit-button').props.accessibilityState.disabled).toBe(false);
  });

  it('requests a reset with the recovery redirect and shows the sent state (AC#6)', async () => {
    mockReset.mockResolvedValueOnce({ data: {}, error: null });
    const { ForgotPasswordScreen } = require('../ui/ForgotPasswordScreen');
    const { RECOVERY_REDIRECT_URL } = require('../hooks/useResetPassword');
    const { getByTestId, queryByTestId } = render(<ForgotPasswordScreen />);

    fireEvent.changeText(getByTestId('email-input'), 'me@example.com');
    fireEvent.press(getByTestId('submit-button'));

    await waitFor(() => expect(queryByTestId('reset-sent')).not.toBeNull());
    expect(mockReset).toHaveBeenCalledWith('me@example.com', {
      redirectTo: RECOVERY_REDIRECT_URL,
    });
  });

  it('surfaces an error banner when the request throws', async () => {
    mockReset.mockRejectedValueOnce(new Error('Network request failed'));
    const { ForgotPasswordScreen } = require('../ui/ForgotPasswordScreen');
    const { getByTestId, queryByTestId } = render(<ForgotPasswordScreen />);

    fireEvent.changeText(getByTestId('email-input'), 'me@example.com');
    fireEvent.press(getByTestId('submit-button'));

    await waitFor(() => expect(queryByTestId('auth-error')).not.toBeNull());
  });
});
