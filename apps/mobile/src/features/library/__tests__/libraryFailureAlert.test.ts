import { Alert } from 'react-native';

import { alertLibraryFailure } from '../libraryFailureAlert';

let alertSpy: jest.SpyInstance;

beforeEach(() => {
  alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => undefined);
});

afterEach(() => {
  alertSpy.mockRestore();
});

describe('alertLibraryFailure — one alert, titled as given, the lead then the closing ask', () => {
  it.each([
    ['auth', 'Could not remove the track. Sign in again, then retry.'],
    ['network', 'Could not remove the track. Please try again.'],
    ['server', 'Could not remove the track. Please try again.'],
    ['not-found', 'Could not remove the track. Please try again.'],
    ['unknown', 'Could not remove the track. Please try again.'],
  ] as const)('a %s failure shows "%s"', (failure, message) => {
    alertLibraryFailure('Delete failed', 'Could not remove the track.', failure);

    expect(alertSpy.mock.calls).toEqual([['Delete failed', message]]);
  });
});
