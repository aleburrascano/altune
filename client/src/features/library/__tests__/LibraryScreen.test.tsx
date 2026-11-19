import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';

import { ApiError } from '@shared/failure';
import { PlaybackContext } from '@shared/playback';
import { libraryKeys } from '@shared/query';

import { LibraryPortProvider, LibraryScreen } from '@features/library';
import { createMemoryLibraryPort } from '@testing/fakes/library/inMemoryLibraryPort';
import { playlist, track } from '@features/library/__tests__/trackMutationFixtures';
import { idlePlayback } from '@testing/queryHarness';
import type { LibraryPort } from '@features/library';
import { warmUpFirstRender } from '@testing/warmUpFirstRender';
import { PlaylistsPortProvider } from '@shared/playlists';
import { createMemoryPlaylistsPort } from '@testing/fakes/playlists/inMemoryPlaylistsPort';
import { AcquisitionPortProvider } from '@shared/acquisition';
import { createMemoryAcquisitionPort } from '@testing/fakes/acquisition/inMemoryAcquisitionPort';
import { usePinnedStore } from '@shared/offline';
import { signInTestUser } from '@testing/signInTestUser';
import { acceptTelemetry, shownFailureSurfaces } from '@features/library/__tests__/failureShown';

const acquisitionPort = createMemoryAcquisitionPort().port;

const playlistsPort = createMemoryPlaylistsPort();

const SKELETON_TILES = 4;
const SERVICE_UNAVAILABLE = 503;

const mockPush = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush, replace: jest.fn(), back: jest.fn() }),
  useSegments: () => ['(tabs)', 'library'],
}));

let client: QueryClient;
let port: LibraryPort;

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={client}>
      <AcquisitionPortProvider port={acquisitionPort}>
        <PlaylistsPortProvider port={playlistsPort}>
          <PlaybackContext.Provider value={idlePlayback}>
            <LibraryPortProvider port={port}>{children}</LibraryPortProvider>
          </PlaybackContext.Provider>
        </PlaylistsPortProvider>
      </AcquisitionPortProvider>
    </QueryClientProvider>
  );
}

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  mockPush.mockReset();
  port = createMemoryLibraryPort();
});

afterEach(() => client.clear());

warmUpFirstRender(async () => {
  const warmUpClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={warmUpClient}>
      <AcquisitionPortProvider port={acquisitionPort}>
        <PlaylistsPortProvider port={playlistsPort}>
          <PlaybackContext.Provider value={idlePlayback}>
            <LibraryPortProvider port={createMemoryLibraryPort()}>
              <LibraryScreen />
            </LibraryPortProvider>
          </PlaybackContext.Provider>
        </PlaylistsPortProvider>
      </AcquisitionPortProvider>
    </QueryClientProvider>,
  );
  await waitFor(() => expect(screen.getByTestId('library-empty')).toBeTruthy());
  warmUpClient.clear();
});

describe('LibraryScreen states', () => {
  it('shows four skeleton tiles while loading', async () => {
    port = { ...createMemoryLibraryPort(), getTracks: () => new Promise(() => undefined) };
    render(<LibraryScreen />, { wrapper });

    fireEvent.press(screen.getByTestId('library-chip-tracks'));

    await waitFor(() =>
      expect(screen.getByTestId('library-loading').children).toHaveLength(SKELETON_TILES),
    );
    expect(screen.queryByTestId('library-error')).toBeNull();
    expect(screen.queryByTestId('library-empty')).toBeNull();
  });

  it('shows the error and retries through the retry button', async () => {
    const memory = createMemoryLibraryPort({ playlists: [playlist([track('t1')])] });
    port = {
      ...memory,
      getPlaylists: jest
        .fn(memory.getPlaylists)
        .mockRejectedValueOnce(new ApiError(SERVICE_UNAVAILABLE, 'down')),
    };
    render(<LibraryScreen />, { wrapper });

    await waitFor(() => expect(screen.getByTestId('library-error')).toBeTruthy());
    expect(screen.queryByText('Road Trip')).toBeNull();

    fireEvent.press(screen.getByTestId('library-retry'));

    await waitFor(() => expect(screen.getByText('Road Trip')).toBeTruthy());
    expect(screen.queryByTestId('library-error')).toBeNull();
  });

  it('shows the empty screen for an empty library and routes to discover', async () => {
    render(<LibraryScreen />, { wrapper });

    await waitFor(() => expect(screen.getByTestId('library-empty')).toBeTruthy());
    fireEvent.press(screen.getByText('Discover Music'));
    expect(mockPush).toHaveBeenCalledWith('/discover');
  });

  it('keeps the ready screen when the active chip is empty but the library is not', async () => {
    port = createMemoryLibraryPort({ tracks: [track('t1')] });
    render(<LibraryScreen />, { wrapper });

    await waitFor(() =>
      expect(client.getQueryData<{ total: number }>(libraryKeys.summary)?.total).toBe(1),
    );
    expect(screen.queryByTestId('library-empty')).toBeNull();
    expect(screen.getByTestId('library-search-input')).toBeTruthy();
  });
});

describe('LibraryScreen selection downloads', () => {
  const realPinMany = usePinnedStore.getState().pinMany;

  afterEach(() => {
    usePinnedStore.setState({ pinMany: realPinMany });
  });

  it('shows the app failure alert when a selected download fails', async () => {
    await signInTestUser();
    acceptTelemetry();
    usePinnedStore.setState({
      pinMany: jest.fn().mockResolvedValue({ requested: 1, failed: 1, refused: null }),
    });
    port = createMemoryLibraryPort({ tracks: [track('t1')] });
    render(<LibraryScreen />, { wrapper });
    fireEvent.press(screen.getByTestId('library-chip-tracks'));
    await waitFor(() => expect(screen.getByTestId('library-row-t1')).toBeTruthy());

    fireEvent(screen.getByTestId('library-row-t1'), 'longPress');
    fireEvent.press(screen.getByText('Download'));

    await waitFor(() => expect(shownFailureSurfaces()).toEqual(['library.pin_batch']));
  });
});
