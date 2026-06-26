/**
 * OAuthButtons — Apple + Google one-tap (AC#10). Verifies both buttons render
 * and dispatch signInWithOAuth with the right provider + callback redirect.
 * Supabase + expo-router mocked; expo-web-browser mocked globally (setup-env).
 */
/* eslint-disable @typescript-eslint/no-require-imports */
import { fireEvent, render, waitFor } from '@testing-library/react-native';

const mockSignInWithOAuth = jest.fn();
jest.mock('../api/supabaseClient', () => ({
  supabase: { auth: { signInWithOAuth: (a: unknown) => mockSignInWithOAuth(a) } },
}));
jest.mock('expo-router', () => ({ useRouter: () => ({ replace: jest.fn(), push: jest.fn() }) }));

beforeEach(() => {
  mockSignInWithOAuth.mockReset().mockResolvedValue({ data: { url: null }, error: null });
});

describe('OAuthButtons', () => {
  it('renders both providers (Apple ships alongside Google per Guideline 4.8)', () => {
    const { OAuthButtons } = require('../ui/OAuthButtons');
    const { getByTestId } = render(<OAuthButtons />);
    expect(getByTestId('oauth-apple')).toBeTruthy();
    expect(getByTestId('oauth-google')).toBeTruthy();
  });

  it('dispatches signInWithOAuth for Apple with the callback redirect', async () => {
    const { OAuthButtons } = require('../ui/OAuthButtons');
    const { OAUTH_REDIRECT_URL } = require('../hooks/useOAuth');
    const { getByTestId } = render(<OAuthButtons />);

    fireEvent.press(getByTestId('oauth-apple'));

    await waitFor(() =>
      expect(mockSignInWithOAuth).toHaveBeenCalledWith({
        provider: 'apple',
        options: { redirectTo: OAUTH_REDIRECT_URL, skipBrowserRedirect: true },
      }),
    );
  });

  it('dispatches signInWithOAuth for Google', async () => {
    const { OAuthButtons } = require('../ui/OAuthButtons');
    const { getByTestId } = render(<OAuthButtons />);

    fireEvent.press(getByTestId('oauth-google'));

    await waitFor(() =>
      expect(mockSignInWithOAuth).toHaveBeenCalledWith(
        expect.objectContaining({ provider: 'google' }),
      ),
    );
  });

  it('surfaces an error banner when the provider URL cannot be obtained', async () => {
    mockSignInWithOAuth.mockResolvedValueOnce({ data: { url: null }, error: { message: 'no' } });
    const { OAuthButtons } = require('../ui/OAuthButtons');
    const { getByTestId, queryByTestId } = render(<OAuthButtons />);

    fireEvent.press(getByTestId('oauth-google'));

    await waitFor(() => expect(queryByTestId('oauth-error')).not.toBeNull());
  });
});
