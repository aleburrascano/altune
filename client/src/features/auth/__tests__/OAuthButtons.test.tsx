import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import * as WebBrowser from 'expo-web-browser';

import { NETWORK_ERROR_COPY } from '@features/auth/domain/authError';
import { OAuthButtons } from '@features/auth/ui/OAuthButtons';

import {
  authPortWrapper,
  createAuthPortMock,
  createSupabaseAuthMock,
} from '@features/auth/__tests__/testUtils/authTestUtils';

jest.mock('expo-router', () => ({ useRouter: () => ({ replace: jest.fn() }) }));
jest.mock('expo-web-browser', () => ({
  maybeCompleteAuthSession: jest.fn(),
  openAuthSessionAsync: jest.fn(),
  dismissAuthSession: jest.fn(),
}));

const { signInWithOAuth } = createSupabaseAuthMock('signInWithOAuth');
const openAuthSessionAsync = jest.mocked(WebBrowser.openAuthSessionAsync);

const authPort = createAuthPortMock();

beforeEach(() => {
  authPort.reportSignInFailure.mockClear();
});

function pressGoogle(): void {
  render(<OAuthButtons />, { wrapper: authPortWrapper(authPort) });
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
    openAuthSessionAsync.mockResolvedValue({
      type: 'cancel' as WebBrowser.WebBrowserResultType.CANCEL,
    });

    pressGoogle();

    await waitFor(() => expect(openAuthSessionAsync).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(screen.getByTestId('oauth-google').props.accessibilityState.disabled).toBe(false),
    );
    expect(screen.queryByTestId('oauth-error')).toBeNull();
  });
});
