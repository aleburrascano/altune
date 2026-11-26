import { fireEvent, waitFor } from '@testing-library/react-native';
import { renderRouter, screen } from 'expo-router/testing-library';

import { OAuthButtons } from '@features/signIn';
import {
  authPortWrapper,
  createAuthBrowserMock,
  createAuthPortMock,
  createSupabaseAuthMock,
} from '@features/signIn/__tests__/testUtils/authTestUtils';

const NETWORK_ERROR_COPY = "Can't reach the server. Check your connection and try again.";
const { signInWithOAuth } = createSupabaseAuthMock('signInWithOAuth');
const authPort = createAuthPortMock();
let browser: ReturnType<typeof createAuthBrowserMock>;

beforeEach(() => {
  authPort.reportSignInFailure.mockClear();
  browser = createAuthBrowserMock();
});

function pressGoogle(): void {
  renderRouter({ index: OAuthButtons }, { wrapper: authPortWrapper(authPort, browser) });
  fireEvent.press(screen.getByTestId('oauth-google'));
}

describe('OAuthButtons: telling the user which failure they hit (#1642)', () => {
  it('names an unreachable server rather than blaming the provider', async () => {
    signInWithOAuth.mockResolvedValue({
      data: null,
      error: { name: 'AuthRetryableFetchError', status: 0, message: 'failed to fetch' },
    });

    pressGoogle();

    expect(await screen.findByTestId('oauth-error')).toHaveTextContent(NETWORK_ERROR_COPY);
  });

  it('falls back to the generic provider copy for any other failure', async () => {
    signInWithOAuth.mockResolvedValue({ data: { url: null }, error: null });

    pressGoogle();

    expect(await screen.findByTestId('oauth-error')).toHaveTextContent(
      "Couldn't sign in with that provider. Please try again.",
    );
  });

  it('shows no banner when the user simply dismissed the browser', async () => {
    signInWithOAuth.mockResolvedValue({
      data: { url: 'https://accounts.google.com/o' },
      error: null,
    });
    browser.openAuthSession.mockResolvedValue({ type: 'cancel' });

    pressGoogle();

    await waitFor(() => expect(browser.openAuthSession).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(screen.getByTestId('oauth-google').props.accessibilityState.disabled).toBe(false),
    );
    expect(screen.queryByTestId('oauth-error')).toBeNull();
  });
});
