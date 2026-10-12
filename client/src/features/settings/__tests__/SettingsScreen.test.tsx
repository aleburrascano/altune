import type { ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react-native';

import { NetworkError } from '@shared/apiClient';
import { RETRY_BACKOFF_BASE_MS } from '@shared/query/retryDelay';

import { signInTestUser } from '@testing/signInTestUser';

import { SettingsScreen } from '@features/settings/ui/SettingsScreen';
import { FeaturePorts } from '@/chrome/FeaturePorts';
import { httpFeaturePorts } from '@/chrome/httpFeaturePorts';

const { __http } = require('@testing/doubles/fetch.js');

jest.mock('@supabase/supabase-js', () => require('@testing/doubles/supabase-js.js'));

const BACKFILL_ROUTE = 'POST /v1/tracks/featured-backfill';
const CLEAR_HISTORY_ROUTE = 'DELETE /v1/discovery/search-history';
const HTTP_ACCEPTED = 202;
const HTTP_UNAVAILABLE = 503;

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return (
    <QueryClientProvider client={client}>
      <FeaturePorts ports={httpFeaturePorts}>{children}</FeaturePorts>
    </QueryClientProvider>
  );
}

const FIVE_ATTEMPTS_OF_BACKOFF_MS = 31 * RETRY_BACKOFF_BASE_MS;

async function elapsePastTheRetries() {
  await act(async () => {
    await jest.advanceTimersByTimeAsync(FIVE_ATTEMPTS_OF_BACKOFF_MS);
  });
}

const backfillRow = () => within(screen.getByTestId('settings-backfill-featured'));
const historyRow = () => within(screen.getByTestId('settings-clear-search-history'));

beforeEach(async () => {
  jest.useFakeTimers();
  await signInTestUser();
  __http.replyAll({ status: HTTP_ACCEPTED });
});

afterEach(() => {
  jest.useRealTimers();
});

describe('settings mutation failures', () => {
  it('backfill: a rejected run shows a failure state, not the idle Run label', async () => {
    __http.fail(BACKFILL_ROUTE, new NetworkError('timeout', 'request timed out'));
    render(<SettingsScreen />, { wrapper });
    expect(backfillRow().getByText('Run')).toBeTruthy();

    fireEvent.press(screen.getByTestId('settings-backfill-featured'));
    await elapsePastTheRetries();

    expect(
      await backfillRow().findByText(
        'Could not reach the server — check your connection and try again.',
      ),
    ).toBeTruthy();
    expect(backfillRow().getByText('Retry')).toBeTruthy();
    expect(backfillRow().queryByText('Run')).toBeNull();
    expect(backfillRow().queryByText('Done')).toBeNull();
  });

  it('backfill: a successful run still reports Done with the counts', async () => {
    __http.reply(BACKFILL_ROUTE, {
      json: { updated: 2, scanned: 9, truncated: false, next_offset: 9 },
    });
    render(<SettingsScreen />, { wrapper });

    fireEvent.press(screen.getByTestId('settings-backfill-featured'));

    expect(await backfillRow().findByText('Done')).toBeTruthy();
    expect(backfillRow().getByText('Updated 2 of 9 tracks')).toBeTruthy();
  });

  it.each([
    ['updated exceeds scanned', { scanned: 3, updated: 12 }],
    ['missing fields', {}],
  ])('backfill: a malformed response (%s) shows a sane fallback', async (_label, json) => {
    __http.reply(BACKFILL_ROUTE, { status: 200, json });
    render(<SettingsScreen />, { wrapper });

    fireEvent.press(screen.getByTestId('settings-backfill-featured'));

    expect(await backfillRow().findByText('Something went wrong — try again.')).toBeTruthy();
    expect(backfillRow().getByText('Retry')).toBeTruthy();
    expect(backfillRow().queryByText(/Updated/)).toBeNull();
  });

  it('clear history: a rejected clear shows a failure state, not Cleared or nothing', async () => {
    __http.reply(CLEAR_HISTORY_ROUTE, {
      status: HTTP_UNAVAILABLE,
      json: { error: { code: 'unavailable', message: 'unavailable' } },
    });
    render(<SettingsScreen />, { wrapper });

    fireEvent.press(screen.getByTestId('settings-clear-search-history'));
    fireEvent.press(screen.getByTestId('settings-confirm-clear-history-confirm'));
    await elapsePastTheRetries();

    expect(
      await historyRow().findByText('The server had a problem — try again in a few minutes.'),
    ).toBeTruthy();
    expect(historyRow().getByText('Failed')).toBeTruthy();
    expect(historyRow().queryByText('Cleared')).toBeNull();
  });

  it('clear history: a successful clear still shows Cleared with no failure copy', async () => {
    __http.reply(CLEAR_HISTORY_ROUTE, { status: 204 });
    render(<SettingsScreen />, { wrapper });

    fireEvent.press(screen.getByTestId('settings-clear-search-history'));
    fireEvent.press(screen.getByTestId('settings-confirm-clear-history-confirm'));

    expect(await historyRow().findByText('Cleared')).toBeTruthy();
    await waitFor(() => expect(historyRow().queryByText('Failed')).toBeNull());
  });
});
