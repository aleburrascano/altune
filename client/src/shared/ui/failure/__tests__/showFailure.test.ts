import { Alert } from 'react-native';

import { recordFailureShown } from '@shared/telemetry/userTelemetry';

import { showFailureAlert } from '@shared/ui/failure/showFailure';
import { showFailureAlert as showFailureAlertFromUi } from '@shared/ui';

jest.mock('@shared/telemetry/userTelemetry', () => ({ recordFailureShown: jest.fn() }));

const recordMock = jest.mocked(recordFailureShown);
let alertSpy: jest.SpyInstance;

beforeEach(() => {
  recordMock.mockClear();
  alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(jest.fn());
});

afterEach(() => {
  alertSpy.mockRestore();
});

describe('showFailureAlert', () => {
  it('records one failure_shown then shows the alert once', () => {
    showFailureAlert({ surface: 'library.retry', title: 'Oops', message: 'Nope', trackId: 't1' });

    expect(recordMock.mock.calls).toEqual([
      [{ surface: 'library.retry', message: 'Nope', track_id: 't1' }],
    ]);
    expect(alertSpy.mock.calls).toEqual([['Oops', 'Nope']]);
    expect(recordMock.mock.invocationCallOrder[0]).toBeLessThan(
      alertSpy.mock.invocationCallOrder[0] as number,
    );
  });

  it('caps the recorded message at 200 chars but alerts the full text', () => {
    const message = 'x'.repeat(250);

    showFailureAlert({ surface: 's', title: 'T', message });

    expect(recordMock.mock.calls[0]?.[0].message).toHaveLength(200);
    expect(alertSpy.mock.calls).toEqual([['T', message]]);
  });

  it('is exported from @shared/ui', () => {
    expect(showFailureAlertFromUi).toBe(showFailureAlert);
  });
});
