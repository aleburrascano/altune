import { render, screen, fireEvent } from '@testing-library/react-native';

import type { SignOutResult } from '@shared/auth/useSignOut';

import { SessionExpiredNotice } from '../SessionExpiredNotice';

const mockSignOut = jest.fn();
let mockState: SignOutResult = { status: 'idle' };

jest.mock('@shared/auth/useSignOut', () => ({
  useSignOut: () => ({ state: mockState, signOut: mockSignOut }),
}));

const ERROR_COPY = "Couldn't reach the server. You've been signed out on this device.";

beforeEach(() => {
  mockSignOut.mockReset();
  mockState = { status: 'idle' };
});

describe('SessionExpiredNotice', () => {
  it('shows the error copy under the button when sign-out failed', () => {
    mockState = { status: 'error', error: new Error('offline') };

    render(<SessionExpiredNotice />);

    expect(screen.getByText(ERROR_COPY)).toBeTruthy();
  });

  it('shows no error copy before a sign-out fails', () => {
    render(<SessionExpiredNotice />);

    expect(screen.queryByText(ERROR_COPY)).toBeNull();
  });

  it('disables the button while signing out', () => {
    mockState = { status: 'loading' };

    render(<SessionExpiredNotice />);
    fireEvent.press(screen.getByTestId('session-expired-signin'));

    expect(screen.getByText('Signing out…')).toBeTruthy();
    expect(mockSignOut).not.toHaveBeenCalled();
  });

  it('signs out when the enabled button is pressed', () => {
    render(<SessionExpiredNotice />);

    fireEvent.press(screen.getByTestId('session-expired-signin'));

    expect(mockSignOut).toHaveBeenCalledTimes(1);
  });
});
