// #2708: a failed next-page fetch set the query's error while its loaded pages stayed put,
// and the views passed that error on, so LibraryScreen replaced the whole list with the
// full-screen error. The error screen is for a list with nothing to show; a failed page
// leaves the loaded rows and a tap-to-retry footer.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import type { ReactElement, ReactNode } from 'react';
import { View } from 'react-native';

import { asTrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';

import { useAlbumsView } from '../hooks/useAlbumsView';
import { useArtistsView } from '../hooks/useArtistsView';
import { useSelection } from '../hooks/useSelection';
import { useTracksView } from '../hooks/useTracksView';
import type { ActiveView } from '../activeView';

const mockGetTracks = jest.fn();
const mockGetLibraryAlbums = jest.fn();
const mockGetLibraryArtists = jest.fn();

jest.mock('@shared/api-client/tracks', () => ({
  getTracks: (params: unknown) => mockGetTracks(params),
  getAllTracks: jest.fn(),
}));
jest.mock('@shared/api-client/library', () => ({
  getLibraryAlbums: (params: unknown) => mockGetLibraryAlbums(params),
  getLibraryArtists: (params: unknown) => mockGetLibraryArtists(params),
}));

const noop = () => undefined;
const PAGE = 50;
const RETRY = 'library-load-more-retry';

const track = (i: number) =>
  ({
    id: asTrackId(`t${i}`),
    title: `Track ${i}`,
    artist: 'An Artist',
    album: null,
    duration_seconds: 180,
    added_at: '2026-01-01T00:00:00Z',
    acquisition_status: 'ready',
    artwork_url: null,
  }) as unknown as TrackResponse;

const album = (i: number) => ({
  key: `al${i}`,
  album: `Album ${i}`,
  artist: 'Radiohead',
  artwork_url: null,
  year: null,
  track_count: 1,
  most_recent_added_at: '2026-01-01T00:00:00Z',
});

const artist = (i: number) => ({
  key: `ar${i}`,
  artist: `Artist ${i}`,
  artwork_url: null,
  track_count: 1,
  most_recent_added_at: '2026-01-01T00:00:00Z',
});

const rows = <T,>(make: (i: number) => T): T[] => Array.from({ length: PAGE }, (_, i) => make(i));

function Screen({ view }: { view: ActiveView }): ReactElement {
  return view.error ? <View testID="library-error" /> : <>{view.content}</>;
}

function TracksScreen(): ReactElement {
  const selection = useSelection();
  const { view } = useTracksView({
    query: '',
    sort: 'recent',
    isActive: true,
    selection,
    queue: {} as never,
    playback: {} as never,
    retryMutation: { isInFlight: () => false } as never,
    onTrackPress: noop,
    onTrackMore: noop,
  });
  return <Screen view={view} />;
}

function AlbumsScreen(): ReactElement {
  const view = useAlbumsView({ query: '', sort: 'recent', isActive: true, onAlbumPress: noop });
  return <Screen view={view} />;
}

function ArtistsScreen(): ReactElement {
  const view = useArtistsView({ query: '', sort: 'az', isActive: true, onArtistPress: noop });
  return <Screen view={view} />;
}

let client: QueryClient;
function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  mockGetTracks.mockReset();
  mockGetLibraryAlbums.mockReset();
  mockGetLibraryArtists.mockReset();
});

afterEach(() => client.clear());

const failure = () => Promise.reject(new Error('page 2 down'));

describe('a library list whose next page fails to load', () => {
  it.each([
    [
      'tracks',
      'library-tracks-list',
      'Track 0',
      TracksScreen,
      () => {
        mockGetTracks.mockResolvedValueOnce({
          items: rows(track),
          total: 200,
          limit: PAGE,
          offset: 0,
          has_more: true,
        });
        mockGetTracks.mockImplementationOnce(failure);
        mockGetTracks.mockResolvedValueOnce({
          items: [track(900)],
          total: 200,
          limit: PAGE,
          offset: PAGE,
          has_more: false,
        });
      },
    ],
    [
      'albums',
      'library-albums-grid',
      'Album 0',
      AlbumsScreen,
      () => {
        mockGetLibraryAlbums.mockResolvedValueOnce({ items: rows(album), total: 200 });
        mockGetLibraryAlbums.mockImplementationOnce(failure);
        mockGetLibraryAlbums.mockResolvedValueOnce({ items: [album(900)], total: 200 });
      },
    ],
    [
      'artists',
      'library-artists-grid',
      'Artist 0',
      ArtistsScreen,
      () => {
        mockGetLibraryArtists.mockResolvedValueOnce({ items: rows(artist), total: 200 });
        mockGetLibraryArtists.mockImplementationOnce(failure);
        mockGetLibraryArtists.mockResolvedValueOnce({ items: [artist(900)], total: 200 });
      },
    ],
  ])('keeps the loaded %s and offers a retry', async (_noun, listId, firstRow, Component, seed) => {
    seed();
    render(<Component />, { wrapper });
    await waitFor(() => expect(screen.getByText(firstRow)).toBeTruthy());

    fireEvent(screen.getByTestId(listId), 'endReached');

    await waitFor(() => expect(screen.getByTestId(RETRY)).toBeTruthy());
    expect(screen.queryByTestId('library-error')).toBeNull();
    expect(screen.getByText(firstRow)).toBeTruthy();

    fireEvent.press(screen.getByTestId(RETRY));

    await waitFor(() => expect(screen.queryByTestId(RETRY)).toBeNull());
    expect(screen.queryByTestId('library-error')).toBeNull();
  });

  it('still shows the error screen when the first page fails', async () => {
    mockGetLibraryAlbums.mockImplementation(failure);
    render(<AlbumsScreen />, { wrapper });

    await waitFor(() => expect(screen.getByTestId('library-error')).toBeTruthy());
  });
});
