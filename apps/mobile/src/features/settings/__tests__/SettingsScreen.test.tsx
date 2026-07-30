import { act, fireEvent, render } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createElement, type ReactElement, type ReactNode } from 'react';

import { usePinnedStore } from '@shared/offline/pinnedStore';
import { ThemeProvider } from '@shared/ui';

import { SettingsScreen } from '../ui/SettingsScreen';

jest.mock('@shared/auth/useSession', () => ({
  useSession: () => ({
    status: 'signed-in',
    session: { user: { email: 'ada@example.com' } },
  }),
}));

const mockSignOut = jest.fn();
jest.mock('@shared/auth/useSignOut', () => ({
  useSignOut: () => ({ state: { kind: 'idle' }, signOut: mockSignOut }),
}));

jest.mock('@shared/offline/pinnedFiles', () => ({
  pinnedBytes: () => 333_447_168,
  formatBytes: () => '318 MB',
}));

jest.mock('@shared/api-client/tracks', () => ({ backfillFeaturedArtists: jest.fn() }));
jest.mock('@shared/api-client/discovery', () => ({ clearSearchHistory: jest.fn() }));
jest.mock('@shared/api-client/feedback', () => ({ submitReport: jest.fn() }));

function renderScreen(): ReturnType<typeof render> {
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }): ReactElement =>
    createElement(
      QueryClientProvider,
      { client: qc },
      createElement(ThemeProvider, null, children),
    );
  return render(<SettingsScreen />, { wrapper });
}

function setDownloads(count: number): void {
  const entries = Object.fromEntries(
    Array.from({ length: count }, (_, i) => [
      `t${i}`,
      { trackId: `t${i}`, status: 'ready' as const },
    ]),
  );
  usePinnedStore.setState({ entries });
}

beforeEach(() => {
  mockSignOut.mockReset();
  usePinnedStore.setState({ entries: {} });
});

describe('SettingsScreen', () => {
  it('offers the report entry point without scrolling past the account', () => {
    const { getByTestId } = renderScreen();

    expect(getByTestId('settings-report-issue')).toBeTruthy();
  });

  it('opens the report dialog from the feedback card', () => {
    const { getByTestId, queryByTestId } = renderScreen();

    expect(queryByTestId('report-issue-message')).toBeNull();
    fireEvent.press(getByTestId('settings-report-issue'));

    expect(getByTestId('report-issue-message')).toBeTruthy();
  });

  it('never signs out on the first tap — it asks first', () => {
    const { getByTestId } = renderScreen();

    fireEvent.press(getByTestId('settings-sign-out'));
    expect(mockSignOut).not.toHaveBeenCalled();

    fireEvent.press(getByTestId('settings-confirm-sign-out-confirm'));
    expect(mockSignOut).toHaveBeenCalledTimes(1);
  });

  it('asks before deleting every download and names what is freed', () => {
    setDownloads(42);
    const unpinAll = jest.fn();
    usePinnedStore.setState({ unpinAll });
    const { getByTestId, getByText } = renderScreen();

    fireEvent.press(getByTestId('settings-remove-downloads'));
    expect(unpinAll).not.toHaveBeenCalled();
    expect(getByText(/42 tracks \(318 MB\)/)).toBeTruthy();

    fireEvent.press(getByTestId('settings-confirm-remove-downloads-confirm'));
    expect(unpinAll).toHaveBeenCalledTimes(1);
  });

  it('hides the remove-downloads row when this device holds nothing', () => {
    const { queryByTestId, getByTestId } = renderScreen();

    expect(queryByTestId('settings-remove-downloads')).toBeNull();
    expect(getByTestId('settings-downloads-usage')).toHaveTextContent(/No downloads/);
  });

  it('follows the pinned store without a new render input', () => {
    const { getByTestId } = renderScreen();

    expect(getByTestId('settings-downloads-usage')).toHaveTextContent(/No downloads/);

    act(() => setDownloads(3));
    expect(getByTestId('settings-downloads-usage')).toHaveTextContent(/3 tracks/);
  });
});
