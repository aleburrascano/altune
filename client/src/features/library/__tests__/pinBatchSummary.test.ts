import { waitFor } from '@testing-library/react-native';
import { showFailureAlert } from '@shared/ui';

import { signInTestUser } from '@testing/signInTestUser';

import {
  reportPinBatch,
  reportStorageFull,
  reportUnpinBatch,
} from '@features/library/view/useLibraryOffline';
import { EVENT_ROUTES } from '@testing/eventRoutes';

const EVENTS = EVENT_ROUTES;

const { __http } = require('@testing/doubles/fetch.js');

const showFailure = jest.fn();

beforeEach(async () => {
  await signInTestUser();
  __http.reply(EVENTS, { status: 204 });
  showFailure.mockReset();
});

describe('pin batch summaries show the user an alert', () => {
  it('reports failed downloads with the count', () => {
    reportPinBatch(showFailure, { requested: 3, failed: 2, refused: null } as never);

    expect(showFailure.mock.calls).toEqual([
      [
        {
          surface: 'library.pin_batch',
          title: 'Some downloads failed',
          message: "2 of 3 downloads failed. Retry them from each track's menu.",
        },
      ],
    ]);
  });

  it('reports downloads that could not be removed', () => {
    reportUnpinBatch(showFailure, { requested: 4, failed: 1 } as never);

    expect(showFailure.mock.calls).toEqual([
      [
        {
          surface: 'library.unpin_batch',
          title: 'Some downloads remain',
          message: '1 of 4 downloads could not be removed. Try removing them again.',
        },
      ],
    ]);
  });

  it('reports storage full', () => {
    reportStorageFull(showFailure);

    expect(showFailure.mock.calls).toEqual([
      [
        {
          surface: 'library.storage_full',
          title: 'Not enough storage',
          message:
            'Downloads are paused because storage is full. Remove some downloads or free up space, then try again.',
        },
      ],
    ]);
  });
});

describe('pin batch summaries record failure_shown', () => {
  it('records one surface per summary', async () => {
    reportPinBatch(showFailureAlert, { requested: 3, failed: 2, refused: null } as never);
    reportUnpinBatch(showFailureAlert, { requested: 4, failed: 1 } as never);
    reportStorageFull(showFailureAlert);

    const expected = ['library.pin_batch', 'library.unpin_batch', 'library.storage_full'];
    await waitFor(() => expect(__http.countFor(EVENTS)).toBe(expected.length));
    const surfaces = (__http.requests as { body: string }[]).map(
      (r) => JSON.parse(r.body).payload.surface,
    );
    expect(surfaces).toEqual(expected);
  });
});
