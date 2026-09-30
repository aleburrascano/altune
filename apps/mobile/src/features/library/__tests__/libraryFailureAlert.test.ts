import { showAlert } from '@shared/ui/dialog/dialog';

import { alertLibraryFailure } from '../libraryFailureAlert';

jest.mock('@shared/ui/dialog/dialog', () => ({ showAlert: jest.fn() }));

const showAlertMock = jest.mocked(showAlert);

beforeEach(() => {
  showAlertMock.mockClear();
});

describe('alertLibraryFailure — one alert, titled as given, the lead then the closing ask', () => {
  it.each([
    ['auth', 'Could not remove the track. Sign in again, then retry.'],
    ['network', 'Could not remove the track. Please try again.'],
    ['server', 'Could not remove the track. Please try again.'],
    ['not-found', 'Could not remove the track. Please try again.'],
    ['unknown', 'Could not remove the track. Please try again.'],
  ] as const)('a %s failure shows "%s"', (failure, message) => {
    alertLibraryFailure(
      'library.delete_track',
      'Delete failed',
      'Could not remove the track.',
      failure,
    );

    expect(showAlertMock.mock.calls).toEqual([['Delete failed', message]]);
  });
});
