import { waitFor } from '@testing-library/react-native';
import { Alert } from 'react-native';

import { signInTestUser } from '@testing/signInTestUser';

import {
  reportPinBatch,
  reportStorageFull,
  reportUnpinBatch,
} from '@features/library/hooks/useLibraryOffline';

const EVENTS = 'POST /v1/discovery/events';

const { __http } = require('@testing/doubles/fetch.js');

let alertSpy: jest.SpyInstance;

beforeEach(async () => {
  await signInTestUser();
  __http.reply(EVENTS, { status: 204 });
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
  it('records one surface per summary', async () => {
    reportPinBatch({ requested: 3, failed: 2, refused: null } as never);
    reportUnpinBatch({ requested: 4, failed: 1 } as never);
    reportStorageFull();

    const expected = ['library.pin_batch', 'library.unpin_batch', 'library.storage_full'];
    await waitFor(() => expect(__http.countFor(EVENTS)).toBe(expected.length));
    const surfaces = (__http.requests as { body: string }[]).map(
      (r) => JSON.parse(r.body).payload.surface,
    );
    expect(surfaces).toEqual(expected);
  });
});
