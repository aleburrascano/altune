import { recordFailureShown } from '@shared/telemetry/userTelemetry';
import { showAlert } from '@shared/ui/dialog/dialog';

import { showFailureAlert } from '../showFailure';
import { showFailureAlert as showFailureAlertFromUi } from '../..';

jest.mock('@shared/telemetry/userTelemetry', () => ({ recordFailureShown: jest.fn() }));
jest.mock('@shared/ui/dialog/dialog', () => ({ showAlert: jest.fn() }));

const recordMock = jest.mocked(recordFailureShown);
const showAlertMock = jest.mocked(showAlert);

beforeEach(() => {
  recordMock.mockClear();
  showAlertMock.mockClear();
});

describe('showFailureAlert', () => {
  it('records one failure_shown then shows the alert once', () => {
    showFailureAlert({ surface: 'library.retry', title: 'Oops', message: 'Nope', trackId: 't1' });

    expect(recordMock.mock.calls).toEqual([
      [{ surface: 'library.retry', message: 'Nope', track_id: 't1' }],
    ]);
    expect(showAlertMock.mock.calls).toEqual([['Oops', 'Nope']]);
    expect(recordMock.mock.invocationCallOrder[0]).toBeLessThan(
      showAlertMock.mock.invocationCallOrder[0] as number,
    );
  });

  it('caps the recorded message at 200 chars but alerts the full text', () => {
    const message = 'x'.repeat(250);

    showFailureAlert({ surface: 's', title: 'T', message });

    expect(recordMock.mock.calls[0]?.[0].message).toHaveLength(200);
    expect(showAlertMock.mock.calls).toEqual([['T', message]]);
  });

  it('is exported from @shared/ui', () => {
    expect(showFailureAlertFromUi).toBe(showFailureAlert);
  });
});
