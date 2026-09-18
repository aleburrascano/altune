import { render, screen } from '@testing-library/react-native';

import { NETWORK_ERROR_COPY } from '../errorReason';
import { useOAuth } from '../hooks/useOAuth';
import { OAuthButtons } from '../ui/OAuthButtons';

jest.mock('../hooks/useOAuth', () => ({ useOAuth: jest.fn() }));

const mockUseOAuth = useOAuth as jest.Mock;

function renderWithState(state: unknown): void {
  mockUseOAuth.mockReturnValue({ state, signInWith: jest.fn() });
  render(<OAuthButtons />);
}

describe('OAuthButtons: telling the user which failure they hit (#1642)', () => {
  it('names an unreachable server rather than blaming the provider', () => {
    renderWithState({ kind: 'error', reason: 'network' });

    expect(screen.getByTestId('oauth-error')).toHaveTextContent(NETWORK_ERROR_COPY);
  });

  it('falls back to the generic provider copy for any other failure', () => {
    renderWithState({ kind: 'error', reason: 'unknown' });

    expect(screen.getByTestId('oauth-error')).toHaveTextContent(
      "Couldn't sign in with that provider. Please try again.",
    );
  });

  it('shows no banner when the user simply dismissed the browser', () => {
    renderWithState({ kind: 'cancelled' });

    expect(screen.queryByTestId('oauth-error')).toBeNull();
  });
});
