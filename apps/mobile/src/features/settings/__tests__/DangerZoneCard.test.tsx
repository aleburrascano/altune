import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';
import { useSignOut, type SignOutResult } from '@shared/auth/useSignOut';

import type { useClearSearchHistory } from '../hooks/useClearSearchHistory';
import { DangerZoneCard } from '../ui/DangerZoneCard';

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { signOut: jest.fn() } },
  clearPersistedAuthSession: jest.fn().mockResolvedValue(undefined),
}));

const mockSignOut = supabase.auth.signOut as jest.Mock;

type ClearHistory = ReturnType<typeof useClearSearchHistory>;

function makeProps(
  over: {
    downloadCount?: number;
    downloadBytes?: number;
    signOutState?: SignOutResult;
    clearHistory?: { isPending?: boolean; isSuccess?: boolean };
  } = {},
) {
  return {
    downloadCount: over.downloadCount ?? 3,
    downloadBytes: over.downloadBytes ?? 12 * 1024 ** 2,
    downloadSize: '12 MB',
    signOutState: over.signOutState ?? { status: 'idle' },
    clearHistory: {
      mutate: jest.fn(),
      isPending: false,
      isSuccess: false,
      ...over.clearHistory,
    } as unknown as ClearHistory,
    unpinAll: jest.fn(),
    signOut: jest.fn().mockResolvedValue(undefined),
  };
}

// A closed RN Modal renders nothing, so presence in the tree is visibility.
const isVisible = (testID: string): boolean => screen.queryByTestId(testID) !== null;

const CONFIRMS = [
  'settings-confirm-remove-downloads',
  'settings-confirm-clear-history',
  'settings-confirm-sign-out',
];

describe('DangerZoneCard', () => {
  it('renders every confirm closed until a row is pressed', () => {
    render(<DangerZoneCard {...makeProps()} />);
    for (const id of CONFIRMS) expect(isVisible(id)).toBe(false);
  });

  it('remove downloads: opens its own confirm with the count copy and calls unpinAll', () => {
    const props = makeProps();
    render(<DangerZoneCard {...props} />);

    expect(screen.getByText('Frees 12 MB · tracks stay in your library')).toBeTruthy();
    fireEvent.press(screen.getByTestId('settings-remove-downloads'));

    expect(isVisible('settings-confirm-remove-downloads')).toBe(true);
    expect(isVisible('settings-confirm-clear-history')).toBe(false);
    expect(isVisible('settings-confirm-sign-out')).toBe(false);
    expect(screen.getByText('Remove all downloads?')).toBeTruthy();
    expect(
      screen.getByText(
        '3 tracks (12 MB) will be deleted from this device. They stay in your library and can be downloaded again.',
      ),
    ).toBeTruthy();

    fireEvent.press(screen.getByTestId('settings-confirm-remove-downloads-confirm'));

    expect(props.unpinAll).toHaveBeenCalledTimes(1);
    expect(props.clearHistory.mutate).not.toHaveBeenCalled();
    expect(props.signOut).not.toHaveBeenCalled();
    expect(isVisible('settings-confirm-remove-downloads')).toBe(false);
  });

  it('clear history: opens its own confirm and calls clearHistory.mutate', () => {
    const props = makeProps();
    render(<DangerZoneCard {...props} />);

    fireEvent.press(screen.getByTestId('settings-clear-search-history'));

    expect(isVisible('settings-confirm-clear-history')).toBe(true);
    expect(screen.getByText('Clear search history?')).toBeTruthy();
    expect(
      screen.getByText('Your recent searches will be deleted from this device and the server.'),
    ).toBeTruthy();

    fireEvent.press(screen.getByTestId('settings-confirm-clear-history-confirm'));

    expect(props.clearHistory.mutate).toHaveBeenCalledTimes(1);
    expect(props.unpinAll).not.toHaveBeenCalled();
    expect(props.signOut).not.toHaveBeenCalled();
    expect(isVisible('settings-confirm-clear-history')).toBe(false);
  });

  it('sign out: opens its own confirm and calls signOut', () => {
    const props = makeProps();
    render(<DangerZoneCard {...props} />);

    fireEvent.press(screen.getByTestId('settings-sign-out'));

    expect(isVisible('settings-confirm-sign-out')).toBe(true);
    expect(screen.getByText('Sign out?')).toBeTruthy();
    expect(
      screen.getByText('Your library stays on the server. Downloads on this device are removed.'),
    ).toBeTruthy();

    fireEvent.press(screen.getByTestId('settings-confirm-sign-out-confirm'));

    expect(props.signOut).toHaveBeenCalledTimes(1);
    expect(props.unpinAll).not.toHaveBeenCalled();
    expect(props.clearHistory.mutate).not.toHaveBeenCalled();
    expect(isVisible('settings-confirm-sign-out')).toBe(false);
  });

  it('hides the remove-downloads row when nothing is downloaded', () => {
    render(<DangerZoneCard {...makeProps({ downloadCount: 0, downloadBytes: 0 })} />);
    expect(screen.queryByTestId('settings-remove-downloads')).toBeNull();
    expect(screen.getByTestId('settings-clear-search-history')).toBeTruthy();
  });

  it('keeps the remove-downloads row when no track is ready but leftover bytes remain', () => {
    render(<DangerZoneCard {...makeProps({ downloadCount: 0 })} />);
    expect(screen.getByTestId('settings-remove-downloads')).toBeTruthy();
  });

  it('keeps an open downloads confirm mounted when the downloads run out', () => {
    const props = makeProps();
    const { rerender } = render(<DangerZoneCard {...props} />);
    fireEvent.press(screen.getByTestId('settings-remove-downloads'));

    rerender(<DangerZoneCard {...props} downloadCount={0} downloadBytes={0} />);

    expect(screen.queryByTestId('settings-remove-downloads')).toBeNull();
    expect(isVisible('settings-confirm-remove-downloads')).toBe(true);
    fireEvent.press(screen.getByTestId('settings-confirm-remove-downloads-confirm'));
    expect(props.unpinAll).toHaveBeenCalledTimes(1);
  });

  it('disables rows while their mutation is pending and shows Cleared on success', () => {
    const props = makeProps({
      signOutState: { status: 'loading' },
      clearHistory: { isPending: true, isSuccess: true },
    });
    render(<DangerZoneCard {...props} />);

    fireEvent.press(screen.getByTestId('settings-clear-search-history'));
    fireEvent.press(screen.getByTestId('settings-sign-out'));

    expect(isVisible('settings-confirm-clear-history')).toBe(false);
    expect(isVisible('settings-confirm-sign-out')).toBe(false);
    expect(screen.getByText('Cleared')).toBeTruthy();
  });
});

describe('DangerZoneCard sign-out failure', () => {
  // #840: a failed sign-out must be visible on the row, not look like nothing happened.
  // #1753: it must also name the cause and leave one redacted line in the log.

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
