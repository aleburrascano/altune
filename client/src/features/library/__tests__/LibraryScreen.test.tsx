import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { fireEvent, render, screen } from '@testing-library/react-native';

import { ApiError } from '@shared/errors';

import { LibraryScreen } from '../ui/LibraryScreen';

const mockPush = jest.fn();
const mockRetry = jest.fn();
const mockActive = jest.fn();
const mockLibraryIsEmpty = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush, replace: jest.fn(), back: jest.fn() }),
  useSegments: () => ['(tabs)', 'library'],
}));
jest.mock('@shared/playback/usePlayback', () => ({
  usePlayback: () => ({ status: 'idle', source: null }),
}));
jest.mock('@shared/playback/useQueuePlayback', () => ({
  useQueuePlayback: () => ({ playFromList: jest.fn() }),
}));
jest.mock('../hooks/useLibraryIsEmpty', () => ({
  useLibraryIsEmpty: () => mockLibraryIsEmpty(),
}));
jest.mock('../hooks/useActiveLibraryView', () => ({
  useActiveLibraryView: () => ({ active: mockActive(), tracks: [], playlists: [] }),
}));

let client: QueryClient;

function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function activeView(over: Record<string, unknown> = {}) {
  return {
    isLoading: false,
    error: null,
    count: 0,
    noun: 'track',
    options: [],
    content: null,
    onRetry: mockRetry,
    ...over,
  };
}

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  mockPush.mockReset();
  mockRetry.mockReset();
  mockLibraryIsEmpty.mockReturnValue(true);
  mockActive.mockReturnValue(activeView());
});

afterEach(() => client.clear());

describe('LibraryScreen states', () => {
  it('shows four skeleton tiles while loading', () => {
    mockActive.mockReturnValue(activeView({ isLoading: true }));
    render(<LibraryScreen />, { wrapper });

    expect(screen.getByTestId('library-loading').children).toHaveLength(4);
    expect(screen.queryByTestId('library-error')).toBeNull();
    expect(screen.queryByTestId('library-empty')).toBeNull();
  });

  it('shows the error and retries through the retry button', () => {
    mockActive.mockReturnValue(activeView({ error: new ApiError(503, 'down') }));
    render(<LibraryScreen />, { wrapper });

    expect(screen.getByTestId('library-error')).toBeTruthy();
    fireEvent.press(screen.getByTestId('library-retry'));
    expect(mockRetry).toHaveBeenCalledTimes(1);
  });

  it('shows the empty screen for an empty library and routes to discover', () => {
    render(<LibraryScreen />, { wrapper });

    expect(screen.getByTestId('library-empty')).toBeTruthy();
    fireEvent.press(screen.getByText('Discover Music'));
    expect(mockPush).toHaveBeenCalledWith('/discover');
  });

  it('keeps the ready screen when the active chip is empty but the library is not', () => {
    mockLibraryIsEmpty.mockReturnValue(false);
    render(<LibraryScreen />, { wrapper });

    expect(screen.queryByTestId('library-empty')).toBeNull();
    expect(screen.getByTestId('library-search-input')).toBeTruthy();
  });
});
