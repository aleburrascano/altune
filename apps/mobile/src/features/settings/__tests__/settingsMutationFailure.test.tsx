// #838: a failed backfill or clear-history mutation must look different from both
// the idle and the success state instead of silently reverting.

import type { ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react-native';

import { ApiError, NetworkError } from '@shared/api-client';
import { clearSearchHistory } from '@shared/api-client/discovery';
import { RETRY_BACKOFF_BASE_MS } from '@shared/query/retryDelay';
import { backfillFeaturedArtists } from '@shared/api-client/tracks';

import { supabase } from '@shared/auth/supabaseClient';

import { SettingsScreen } from '../ui/SettingsScreen';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

jest.mock('@shared/api-client/tracks', () => ({
  ...jest.requireActual('@shared/api-client/tracks'),
  backfillFeaturedArtists: jest.fn(),
}));
jest.mock('@shared/api-client/discovery', () => ({
  ...jest.requireActual('@shared/api-client/discovery'),
  clearSearchHistory: jest.fn(),
}));
jest.mock('@shared/auth/useSignOut', () => ({
  useSignOut: () => ({ state: { status: 'idle' }, signOut: jest.fn() }),
}));
jest.mock('../hooks/useAccountEmail', () => ({ useAccountEmail: () => 'me@example.com' }));
jest.mock('../hooks/useDownloadStats', () => ({
  ...jest.requireActual('../hooks/useDownloadStats'),
  useDownloadStats: () => ({
    downloadCount: 0,
    downloadBytes: 0,
    downloadSize: '0 B',
    usageLabel: 'No downloads on this device',
    usageDetail: undefined,
  }),
}));
jest.mock('@shared/offline/pinnedStore', () => ({
  usePinnedStore: (select: (s: { unpinAll: () => void }) => unknown) =>
    select({ unpinAll: jest.fn() }),
}));

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    // The hooks opt into transient retries (#841) on a backoff of their own (#1756),
    // overriding both of these defaults.
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

// 1+2+4+8+16s: the un-jittered ceiling of the five attempts the hooks allow.
const FIVE_ATTEMPTS_OF_BACKOFF_MS = 31 * RETRY_BACKOFF_BASE_MS;

// A retryable failure only reaches its final state once the backoff has run out.
async function elapsePastTheRetries() {
  await act(async () => {
    await jest.advanceTimersByTimeAsync(FIVE_ATTEMPTS_OF_BACKOFF_MS);
  });
}

const backfillRow = () => within(screen.getByTestId('settings-backfill-featured'));
const historyRow = () => within(screen.getByTestId('settings-clear-search-history'));

beforeEach(() => {
  jest.useFakeTimers();
  jest.mocked(backfillFeaturedArtists).mockReset();
  jest.mocked(clearSearchHistory).mockReset();
  jest.mocked(supabase.auth.getSession).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  } as never);
});

afterEach(() => {
  jest.useRealTimers();
});

describe('settings mutation failures (#838)', () => {
  it('backfill: a rejected run shows a failure state, not the idle Run label', async () => {
    jest
      .mocked(backfillFeaturedArtists)
      .mockRejectedValue(new NetworkError('timeout', 'request timed out'));
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
    jest.mocked(backfillFeaturedArtists).mockResolvedValue({ updated: 2, scanned: 9 });
    render(<SettingsScreen />, { wrapper });

    fireEvent.press(screen.getByTestId('settings-backfill-featured'));

    expect(await backfillRow().findByText('Done')).toBeTruthy();
    expect(backfillRow().getByText('Updated 2 of 9 tracks')).toBeTruthy();
  });

  // #843: the real client over a malformed 200 body must never render "Updated 12 of 3".
  it.each([
    ['updated exceeds scanned', { scanned: 3, updated: 12 }],
    ['missing fields', {}],
  ])('backfill: a malformed response (%s) shows a sane fallback', async (_label, json) => {
    const actual = jest.requireActual('@shared/api-client/tracks');
    jest.mocked(backfillFeaturedArtists).mockImplementation(actual.backfillFeaturedArtists);
    __http.reply('POST /v1/tracks/featured-backfill', { status: 200, json });
    render(<SettingsScreen />, { wrapper });

    fireEvent.press(screen.getByTestId('settings-backfill-featured'));

    expect(await backfillRow().findByText('Something went wrong — try again.')).toBeTruthy();
    expect(backfillRow().getByText('Retry')).toBeTruthy();
    expect(backfillRow().queryByText(/Updated/)).toBeNull();
  });

  it('clear history: a rejected clear shows a failure state, not Cleared or nothing', async () => {
    jest.mocked(clearSearchHistory).mockRejectedValue(new ApiError(503, 'unavailable'));
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
    jest.mocked(clearSearchHistory).mockResolvedValue(undefined);
    render(<SettingsScreen />, { wrapper });

    fireEvent.press(screen.getByTestId('settings-clear-search-history'));
    fireEvent.press(screen.getByTestId('settings-confirm-clear-history-confirm'));

    expect(await historyRow().findByText('Cleared')).toBeTruthy();
    await waitFor(() => expect(historyRow().queryByText('Failed')).toBeNull());
  });
});
