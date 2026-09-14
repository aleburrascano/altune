import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';
import { useSignOut } from '@shared/auth/useSignOut';

import type { useClearSearchHistory } from '../hooks/useClearSearchHistory';
import { DangerZoneCard } from '../ui/DangerZoneCard';

// #840: a failed sign-out must be visible on the row, not look like nothing happened.

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { signOut: jest.fn() } },
}));

const mockSignOut = supabase.auth.signOut as jest.Mock;

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

beforeEach(() => {
  mockSignOut.mockReset();
});

describe('DangerZoneCard sign-out failure', () => {
  it.each([
    ['rejects', () => mockSignOut.mockRejectedValue(new Error('network request failed'))],
    [
      'resolves with an error',
      () => mockSignOut.mockResolvedValue({ error: { message: 'invalid_grant', status: 400 } }),
    ],
  ] as const)(
    'shows Failed and retry copy when supabase.auth.signOut() %s',
    async (_label, arrange) => {
      arrange();
      renderCard();
      expect(screen.queryByText('Failed')).toBeNull();

      confirmSignOut();

      expect(await screen.findByText('Failed')).toBeTruthy();
      expect(
        screen.getByText('Could not sign out — check your connection and try again.'),
      ).toBeTruthy();
      // The row stays usable so the user can retry.
      fireEvent.press(screen.getByTestId('settings-sign-out'));
      expect(screen.queryByTestId('settings-confirm-sign-out')).not.toBeNull();
    },
  );

  it('shows no failure state when sign-out succeeds', async () => {
    mockSignOut.mockResolvedValue({ error: null });
    renderCard();

    confirmSignOut();

    await waitFor(() => expect(mockSignOut).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(screen.getByTestId('settings-sign-out')).toBeTruthy());
    expect(screen.queryByText('Failed')).toBeNull();
    expect(
      screen.queryByText('Could not sign out — check your connection and try again.'),
    ).toBeNull();
  });
});
