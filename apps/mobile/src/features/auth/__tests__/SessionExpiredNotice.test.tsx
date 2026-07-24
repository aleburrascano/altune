import { render, screen, userEvent } from '@testing-library/react-native';

const mockSignOut = jest.fn(async () => undefined);
jest.mock('@shared/auth/useSignOut', () => ({
  useSignOut: () => ({ state: { kind: 'idle' }, signOut: mockSignOut }),
}));

beforeEach(() => {
  mockSignOut.mockClear();
});

describe('SessionExpiredNotice', () => {
  it('signs out when the user chooses to re-authenticate', async () => {
    const { SessionExpiredNotice } = require('../ui/SessionExpiredNotice');
    render(<SessionExpiredNotice />);

    await userEvent.setup().press(screen.getByTestId('session-expired-signin'));

    expect(mockSignOut).toHaveBeenCalledTimes(1);
  });

  it('tells the user their data is intact', () => {
    const { SessionExpiredNotice } = require('../ui/SessionExpiredNotice');
    render(<SessionExpiredNotice />);

    expect(screen.getByText(/Nothing has been lost/i)).toBeTruthy();
  });
});
