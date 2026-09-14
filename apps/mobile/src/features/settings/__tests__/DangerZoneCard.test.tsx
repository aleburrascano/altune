import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react-native';

import type { SignOutResult } from '@shared/auth/useSignOut';

import type { useClearSearchHistory } from '../hooks/useClearSearchHistory';
import { DangerZoneCard } from '../ui/DangerZoneCard';

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
    signOutState: over.signOutState ?? { kind: 'idle' },
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
      signOutState: { kind: 'pending' },
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
