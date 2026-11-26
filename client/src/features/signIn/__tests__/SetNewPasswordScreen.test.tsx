import { fireEvent, screen, waitFor } from '@testing-library/react-native';
import { renderRouter } from 'expo-router/testing-library';
import { Text } from 'react-native';

import { SetNewPasswordScreen } from '@features/signIn';
import {
  authPortWrapper,
  createSupabaseAuthMock,
} from '@features/signIn/__tests__/testUtils/authTestUtils';
import { recordLogs } from '@testing/fakes/log/inMemoryLogger';
import { recordDialogs, type RecordedDialogs } from '@testing/recordDialogs';

const ROUTES = {
  index: SetNewPasswordScreen,
  library: () => <Text>library-screen</Text>,
};

const { updateUser, signOut } = createSupabaseAuthMock('updateUser', 'signOut');

let dialogs: RecordedDialogs;

beforeEach(() => {
  dialogs = recordDialogs();
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
    recordLogs();
    signOut.mockResolvedValue({ error: { name: 'AuthApiError', status: 500, message: 'down' } });
    const router = renderRouter(ROUTES, { wrapper: authPortWrapper() });

    submitNewPassword();

    await waitFor(() => expect(router.getPathname()).toBe('/library'));
    expect(dialogs.alerts).toEqual([
      [expect.any(String), expect.stringContaining('may still be signed in')],
    ]);
  });

  it('routes without a warning when the other sessions were revoked', async () => {
    const router = renderRouter(ROUTES, { wrapper: authPortWrapper() });

    submitNewPassword();

    await waitFor(() => expect(router.getPathname()).toBe('/library'));
    expect(updateUser).toHaveBeenCalledWith({ password: 'Hunter2!pass' });
    expect(dialogs.alerts).toEqual([]);
  });

  it('does not route before the update finishes', async () => {
    updateUser.mockReturnValue(new Promise(() => undefined));
    const router = renderRouter(ROUTES, { wrapper: authPortWrapper() });

    submitNewPassword();

    await waitFor(() => expect(updateUser).toHaveBeenCalledTimes(1));
    expect(router.getPathname()).toBe('/');
  });
});
