import { QueryClient } from '@tanstack/react-query';
import { act, fireEvent, render, screen } from '@testing-library/react-native';

import { makeWrapper } from '@testing/makeWrapper';
import { signInTestUser } from '@testing/signInTestUser';
import { SettingsRoute } from '@/mobile';

const { __http } = require('@testing/doubles/fetch.js');

const FIRST_BACKOFF_WINDOW_MS = 1_000;
const ATTEMPT_AND_ONE_RETRY = 2;

type WireRequest = { method: string; path: string };

function backfillRequests(): WireRequest[] {
  return __http.requests.filter(
    (r: WireRequest) => `${r.method} ${r.path}` === 'POST /v1/tracks/featured-backfill',
  );
}

beforeEach(async () => {
  jest.useFakeTimers();
  await signInTestUser();
});

afterEach(() => {
  jest.useRealTimers();
});

describe('SettingsRoute', () => {
  it('gives the settings screen the retrying options the app ships: a 502 on the backfill is attempted again within the first backoff window', async () => {
    __http.replyOnce('POST /v1/tracks/featured-backfill', {
      status: 502,
      json: { error: { code: 'bad_gateway', message: 'bad gateway' } },
    });
    __http.reply('POST /v1/tracks/featured-backfill', {
      json: { updated: 1, scanned: 1, truncated: false, next_offset: 1 },
    });
    render(<SettingsRoute />, { wrapper: makeWrapper(new QueryClient()) });

    fireEvent.press(screen.getByTestId('settings-backfill-featured'));
    await act(async () => {
      await jest.advanceTimersByTimeAsync(FIRST_BACKOFF_WINDOW_MS);
    });

    expect(backfillRequests()).toHaveLength(ATTEMPT_AND_ONE_RETRY);
  });
});
