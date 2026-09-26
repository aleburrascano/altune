import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { within } from '@testing-library/react-native';

import { ApiError } from '@shared/api-client';
import { supabase } from '@shared/auth/supabaseClient';
import { useSignOut, type SignOutResult } from '@shared/auth/useSignOut';

import { downloadUsage } from '../downloadStatsModel';
import type { ClearHistoryState } from '../ui/dangerZoneActions';
import { DangerZoneCard } from '../ui/DangerZoneCard';

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { signOut: jest.fn() } },
  clearPersistedAuthSession: jest.fn().mockResolvedValue(undefined),
}));

const mockSignOut = supabase.auth.signOut as jest.Mock;

function makeProps(
  over: {
    downloadCount?: number;
    downloadBytes?: number;
    signOutState?: SignOutResult;
    clearHistory?: Partial<ClearHistoryState>;
  } = {},
) {
  const downloadCount = over.downloadCount ?? 3;
  const downloadBytes = over.downloadBytes ?? 12 * 1024 ** 2;
  return {
    downloads: {
      stats: {
        downloadCount,
        downloadBytes,
        downloadSize: '12 MB',
        usage: downloadUsage(downloadCount, downloadBytes),
        usageLabel: '',
        usageDetail: undefined,
      },
      unpinAll: jest.fn(),
    },
    signOutState: over.signOutState ?? { status: 'idle' },
    clearHistory: {
      mutate: jest.fn(),
      isPending: false,
      isError: false,
      isSuccess: false,
      error: undefined,
      ...over.clearHistory,
    } satisfies ClearHistoryState,
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

    expect(props.downloads.unpinAll).toHaveBeenCalledTimes(1);
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
    expect(props.downloads.unpinAll).not.toHaveBeenCalled();
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
    expect(props.downloads.unpinAll).not.toHaveBeenCalled();
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

    rerender(
      <DangerZoneCard
        {...props}
        downloads={{ ...props.downloads, stats: { ...props.downloads.stats, downloadCount: 0, downloadBytes: 0, usage: 'none' } }}
      />,
    );

    expect(screen.queryByTestId('settings-remove-downloads')).toBeNull();
    expect(isVisible('settings-confirm-remove-downloads')).toBe(true);
    fireEvent.press(screen.getByTestId('settings-confirm-remove-downloads-confirm'));
    expect(props.downloads.unpinAll).toHaveBeenCalledTimes(1);
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

  const clearHistory: ClearHistoryState = {
    mutate: jest.fn(),
    isPending: false,
    isSuccess: false,
    isError: false,
    error: undefined,
  };

  function Harness(): React.ReactElement {
    const { state, signOut } = useSignOut();
    return (
      <DangerZoneCard
        downloads={{
          stats: {
            downloadCount: 0,
            downloadBytes: 0,
            downloadSize: '0 B',
            usage: 'none',
            usageLabel: 'No downloads on this device',
            usageDetail: undefined,
          },
          unpinAll: jest.fn(),
        }}
        signOutState={state}
        clearHistory={clearHistory}
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

describe('DangerZoneCard probe: rows across download usage, outcomes and mutation states', () => {
  const rowOf = (testID: string) => within(screen.getByTestId(testID));

  it('marks the downloads row Failed with retry copy after a partial removal', () => {
    const props = makeProps();
    render(
      <DangerZoneCard {...props} downloads={{ ...props.downloads, lastUnpinAll: 'partial' }} />,
    );

    expect(rowOf('settings-remove-downloads').getByText('Failed')).toBeTruthy();
    expect(
      rowOf('settings-remove-downloads').getByText(
        "Some downloads couldn't be removed — try again.",
      ),
    ).toBeTruthy();
    expect(rowOf('settings-clear-search-history').queryByText('Failed')).toBeNull();
    expect(rowOf('settings-sign-out').queryByText('Failed')).toBeNull();
  });

  it('keeps a partially failed downloads row pressable so the user can retry', () => {
    const props = makeProps();
    render(
      <DangerZoneCard {...props} downloads={{ ...props.downloads, lastUnpinAll: 'partial' }} />,
    );

    fireEvent.press(screen.getByTestId('settings-remove-downloads'));
    fireEvent.press(screen.getByTestId('settings-confirm-remove-downloads-confirm'));

    expect(props.downloads.unpinAll).toHaveBeenCalledTimes(1);
  });

  it('shows the partial-removal failure on a row holding only leftover files', () => {
    const props = makeProps({ downloadCount: 0 });
    render(
      <DangerZoneCard {...props} downloads={{ ...props.downloads, lastUnpinAll: 'partial' }} />,
    );

    expect(rowOf('settings-remove-downloads').getByText('Failed')).toBeTruthy();
  });

  it('shows no status on the downloads row after every file was removed', () => {
    const props = makeProps();
    render(
      <DangerZoneCard
        {...props}
        downloads={{ ...props.downloads, lastUnpinAll: 'all-removed' }}
      />,
    );

    expect(screen.queryByText('Failed')).toBeNull();
    expect(screen.getByText('Frees 12 MB · tracks stay in your library')).toBeTruthy();
  });

  it('asks to delete leftover files, not tracks, when only leftover bytes remain', () => {
    const props = makeProps({ downloadCount: 0 });
    render(<DangerZoneCard {...props} />);

    fireEvent.press(screen.getByTestId('settings-remove-downloads'));

    expect(
      screen.getByText('Leftover download files (12 MB) will be deleted from this device.'),
    ).toBeTruthy();
    fireEvent.press(screen.getByTestId('settings-confirm-remove-downloads-confirm'));
    expect(props.downloads.unpinAll).toHaveBeenCalledTimes(1);
  });

  it('keeps the downloads row for ready tracks that report zero bytes', () => {
    render(<DangerZoneCard {...makeProps({ downloadCount: 2, downloadBytes: 0 })} />);

    expect(screen.getByTestId('settings-remove-downloads')).toBeTruthy();
  });

  it('closes each confirm on Cancel without running its action', () => {
    const props = makeProps();
    render(<DangerZoneCard {...props} />);

    for (const [row, confirm] of [
      ['settings-remove-downloads', 'settings-confirm-remove-downloads'],
      ['settings-clear-search-history', 'settings-confirm-clear-history'],
      ['settings-sign-out', 'settings-confirm-sign-out'],
    ] as const) {
      fireEvent.press(screen.getByTestId(row));
      expect(isVisible(confirm)).toBe(true);
      fireEvent.press(screen.getByText('Cancel'));
      expect(isVisible(confirm)).toBe(false);
    }

    expect(props.downloads.unpinAll).not.toHaveBeenCalled();
    expect(props.clearHistory.mutate).not.toHaveBeenCalled();
    expect(props.signOut).not.toHaveBeenCalled();
  });

  it('disables only the clear-history row while its clear is pending', () => {
    render(<DangerZoneCard {...makeProps({ clearHistory: { isPending: true } })} />);

    fireEvent.press(screen.getByTestId('settings-clear-search-history'));
    expect(isVisible('settings-confirm-clear-history')).toBe(false);
    expect(screen.queryByText('Cleared')).toBeNull();
    expect(screen.queryByText('Failed')).toBeNull();

    fireEvent.press(screen.getByTestId('settings-sign-out'));
    expect(isVisible('settings-confirm-sign-out')).toBe(true);
  });

  it('disables only the sign-out row while sign-out is in flight', () => {
    render(<DangerZoneCard {...makeProps({ signOutState: { status: 'loading' } })} />);

    fireEvent.press(screen.getByTestId('settings-sign-out'));
    expect(isVisible('settings-confirm-sign-out')).toBe(false);

    fireEvent.press(screen.getByTestId('settings-clear-search-history'));
    expect(isVisible('settings-confirm-clear-history')).toBe(true);
  });

  it.each([
    [
      'a refused session',
      new ApiError(401, 'unauthorized'),
      'Your session has expired — sign in again and retry.',
    ],
    ['an unrecognised error', new Error('boom'), 'Something went wrong — try again.'],
  ])(
    'marks only the clear-history row Failed with copy for %s and leaves it retryable',
    (_label, error, copy) => {
      const props = makeProps({ clearHistory: { isError: true, error } });
      render(<DangerZoneCard {...props} />);

      expect(rowOf('settings-clear-search-history').getByText('Failed')).toBeTruthy();
      expect(rowOf('settings-clear-search-history').getByText(copy)).toBeTruthy();
      expect(rowOf('settings-clear-search-history').queryByText('Cleared')).toBeNull();
      expect(rowOf('settings-sign-out').queryByText('Failed')).toBeNull();

      fireEvent.press(screen.getByTestId('settings-clear-search-history'));
      fireEvent.press(screen.getByTestId('settings-confirm-clear-history-confirm'));
      expect(props.clearHistory.mutate).toHaveBeenCalledTimes(1);
    },
  );

  it('shows Cleared on a finished clear with no failure copy', () => {
    render(<DangerZoneCard {...makeProps({ clearHistory: { isSuccess: true } })} />);

    expect(rowOf('settings-clear-search-history').getByText('Cleared')).toBeTruthy();
    expect(screen.queryByText('Failed')).toBeNull();
  });

  it('marks only the sign-out row Failed when handed a failed sign-out state', () => {
    const props = makeProps({
      signOutState: { status: 'error', error: new ApiError(503, 'unavailable') },
    });
    render(<DangerZoneCard {...props} />);

    expect(rowOf('settings-sign-out').getByText('Failed')).toBeTruthy();
    expect(
      rowOf('settings-sign-out').getByText(
        'The server had a problem — try again in a few minutes.',
      ),
    ).toBeTruthy();
    expect(rowOf('settings-clear-search-history').queryByText('Failed')).toBeNull();
    expect(rowOf('settings-remove-downloads').queryByText('Failed')).toBeNull();
  });
});
