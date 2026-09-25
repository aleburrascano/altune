import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';
import { useSignOut } from '@shared/auth/useSignOut';

import type { useClearSearchHistory } from '../hooks/useClearSearchHistory';
import { DangerZoneCard } from '../ui/DangerZoneCard';

// #840: a failed sign-out must be visible on the row, not look like nothing happened.
// #1753: it must also name the cause and leave one redacted line in the log.

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { signOut: jest.fn() } },
}));

const mockSignOut = supabase.auth.signOut as jest.Mock;

// The shapes supabase-js hands back from signOut(), one per cause.
const unreachableAuthServer = {
  name: 'AuthRetryableFetchError',
  message: 'Network request failed',
  status: 0,
};
const refusedSession = { name: 'AuthApiError', message: 'invalid_grant', status: 401 };
const brokenAuthServer = { name: 'AuthApiError', message: 'unexpected_failure', status: 503 };

const clearHistory = {
  mutate: jest.fn(),
  isPending: false,
  isSuccess: false,
  isError: false,
} as unknown as ReturnType<typeof useClearSearchHistory>;

function Harness(): React.ReactElement {
  const { state, signOut } = useSignOut();
  return (
    <DangerZoneCard
      downloadCount={0}
      downloadBytes={0}
      downloadSize="0 B"
      signOutState={state}
      clearHistory={clearHistory}
      unpinAll={jest.fn()}
      signOut={signOut}
    />
  );
}

function renderCard(): void {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <Harness />
    </QueryClientProvider>,
  );
}

function confirmSignOut(): void {
  fireEvent.press(screen.getByTestId('settings-sign-out'));
  fireEvent.press(screen.getByTestId('settings-confirm-sign-out-confirm'));
}

let warn: jest.SpyInstance;

beforeEach(() => {
  mockSignOut.mockReset();
  warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  warn.mockRestore();
});

describe('DangerZoneCard sign-out failure', () => {
  it.each([
    [
      'the auth server cannot be reached',
      () => mockSignOut.mockResolvedValue({ error: unreachableAuthServer }),
      'Could not reach the server — check your connection and try again.',
      { failure: 'transport' },
    ],
    [
      'the session is already refused',
      () => mockSignOut.mockResolvedValue({ error: refusedSession }),
      'Your session has expired — sign in again and retry.',
      { status: 401 },
    ],
    [
      'the auth server is broken',
      () => mockSignOut.mockResolvedValue({ error: brokenAuthServer }),
      'The server had a problem — try again in a few minutes.',
      { status: 503 },
    ],
    [
      'signOut() throws something unrecognised',
      () => mockSignOut.mockRejectedValue(new Error('boom')),
      'Something went wrong — try again.',
      { failure: 'unknown' },
    ],
  ] as const)(
    'shows Failed with copy for the cause and logs it when %s',
    async (_label, arrange, expectedDetail, expectedLogFields) => {
      arrange();
      renderCard();
      expect(screen.queryByText('Failed')).toBeNull();

      confirmSignOut();

      expect(await screen.findByText('Failed')).toBeTruthy();
      expect(screen.getByText(expectedDetail)).toBeTruthy();
      expect(warn).toHaveBeenCalledWith('[auth] sign out failed', expectedLogFields);
    },
  );

  it('keeps the auth server message out of the logged line', async () => {
    mockSignOut.mockResolvedValue({ error: refusedSession });
    renderCard();

    confirmSignOut();

    expect(await screen.findByText('Failed')).toBeTruthy();
    expect(warn).toHaveBeenCalledTimes(1);
    expect(JSON.stringify(warn.mock.calls)).not.toContain('invalid_grant');
  });

  it('leaves the row usable so the user can retry after a failure', async () => {
    mockSignOut.mockRejectedValue(new Error('network request failed'));
    renderCard();

    confirmSignOut();

    expect(await screen.findByText('Failed')).toBeTruthy();
    fireEvent.press(screen.getByTestId('settings-sign-out'));
    expect(screen.queryByTestId('settings-confirm-sign-out')).not.toBeNull();
  });

  it('shows no failure state and logs nothing when sign-out succeeds', async () => {
    mockSignOut.mockResolvedValue({ error: null });
    renderCard();

    confirmSignOut();

    await waitFor(() => expect(mockSignOut).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(screen.getByTestId('settings-sign-out')).toBeTruthy());
    expect(screen.queryByText('Failed')).toBeNull();
    expect(warn).not.toHaveBeenCalled();
  });
});
