import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';

import { showAlert } from '@shared/ui/dialog/dialog';

import { SetNewPasswordScreen } from '../SetNewPasswordScreen';

const mockReplace = jest.fn();
const mockUpdatePassword = jest.fn();
let mockState: unknown = { kind: 'idle' };

jest.mock('expo-router', () => ({ useRouter: () => ({ replace: mockReplace }) }));
jest.mock('@shared/ui/dialog/dialog', () => ({ showAlert: jest.fn() }));
jest.mock('../../recoveryUnlock', () => ({ clearRecoveryUnlock: jest.fn() }));
jest.mock('../../hooks/useUpdatePassword', () => ({
  useUpdatePassword: () => ({ state: mockState, updatePassword: mockUpdatePassword }),
}));

beforeEach(() => {
  jest.clearAllMocks();
  mockState = { kind: 'idle' };
});

describe('SetNewPasswordScreen', () => {
  it('warns that other devices may still be signed in, then routes to the library', async () => {
    mockState = { kind: 'ok', othersRevoked: false };

    render(<SetNewPasswordScreen />);

    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith('/library'));
    expect(showAlert).toHaveBeenCalledWith(
      expect.any(String),
      expect.stringContaining('may still be signed in'),
    );
  });

  it('routes without a warning when the other sessions were revoked', async () => {
    mockState = { kind: 'ok', othersRevoked: true };

    render(<SetNewPasswordScreen />);

    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith('/library'));
    expect(showAlert).not.toHaveBeenCalled();
  });

  it('does not route before the update finishes', () => {
    render(<SetNewPasswordScreen />);
    fireEvent.press(screen.getByTestId('submit-button'));

    expect(mockReplace).not.toHaveBeenCalled();
  });
});
