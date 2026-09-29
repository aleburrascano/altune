import { showAlert } from '@shared/ui/dialog/dialog';

import { reportPinBatch, reportStorageFull, reportUnpinBatch } from '../pinBatchSummary';

jest.mock('@shared/ui/dialog/dialog', () => ({ showAlert: jest.fn() }));

const showAlertMock = jest.mocked(showAlert);

beforeEach(() => {
  showAlertMock.mockClear();
});

describe('pin batch summaries alert through the dialog port', () => {
  it('reports failed downloads with the count', () => {
    reportPinBatch({ requested: 3, failed: 2, refused: null } as never);

    expect(showAlertMock.mock.calls).toEqual([
      ['Some downloads failed', "2 of 3 downloads failed. Retry them from each track's menu."],
    ]);
  });

  it('reports downloads that could not be removed', () => {
    reportUnpinBatch({ requested: 4, failed: 1 } as never);

    expect(showAlertMock.mock.calls).toEqual([
      ['Some downloads remain', '1 of 4 downloads could not be removed. Try removing them again.'],
    ]);
  });

  it('reports storage full', () => {
    reportStorageFull();

    expect(showAlertMock.mock.calls).toEqual([
      [
        'Not enough storage',
        'Downloads are paused because storage is full. Remove some downloads or free up space, then try again.',
      ],
    ]);
  });
});
