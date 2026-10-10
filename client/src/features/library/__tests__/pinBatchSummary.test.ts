import { Alert } from 'react-native';

import { recordFailureShown } from '@shared/telemetry/userTelemetry';

import {
  reportPinBatch,
  reportStorageFull,
  reportUnpinBatch,
} from '@features/library/hooks/useLibraryOffline';

jest.mock('@shared/telemetry/userTelemetry', () => ({ recordFailureShown: jest.fn() }));

let alertSpy: jest.SpyInstance;

beforeEach(() => {
  alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => undefined);
});

afterEach(() => {
  alertSpy.mockRestore();
});

describe('pin batch summaries show the user an alert', () => {
  it('reports failed downloads with the count', () => {
    reportPinBatch({ requested: 3, failed: 2, refused: null } as never);

    expect(alertSpy.mock.calls).toEqual([
      ['Some downloads failed', "2 of 3 downloads failed. Retry them from each track's menu."],
    ]);
  });

  it('reports downloads that could not be removed', () => {
    reportUnpinBatch({ requested: 4, failed: 1 } as never);

    expect(alertSpy.mock.calls).toEqual([
      ['Some downloads remain', '1 of 4 downloads could not be removed. Try removing them again.'],
    ]);
  });

  it('reports storage full', () => {
    reportStorageFull();

    expect(alertSpy.mock.calls).toEqual([
      [
        'Not enough storage',
        'Downloads are paused because storage is full. Remove some downloads or free up space, then try again.',
      ],
    ]);
  });
});

describe('pin batch summaries record failure_shown', () => {
  it('records one surface per summary', () => {
    jest.mocked(recordFailureShown).mockClear();
    reportPinBatch({ requested: 3, failed: 2, refused: null } as never);
    reportUnpinBatch({ requested: 4, failed: 1 } as never);
    reportStorageFull();

    expect(jest.mocked(recordFailureShown).mock.calls.map(([f]) => f.surface)).toEqual([
      'library.pin_batch',
      'library.unpin_batch',
      'library.storage_full',
    ]);
  });
});
