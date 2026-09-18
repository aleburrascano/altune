// #1707: the albums and artists grids fetched one page and never asked for another, so a
// library larger than the server's default page ended at row 50 with nothing on screen
// saying so. The fake server here holds the real one's contract: a caller that sends no
// limit is served 50 rows, and offset walks the list.
//
// The count beside the sort control is what the assertions read: a virtualized grid renders
// only its window, so the rows on screen count the window, never the library.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import type { ReactElement, ReactNode } from 'react';
import { FlatList } from 'react-native';

import type { AlbumGroup, ArtistGroup, LibraryQuery } from '@shared/api-client/library';

import { useAlbumsView } from '../hooks/useAlbumsView';
import { useArtistsView } from '../hooks/useArtistsView';
import { SortControl } from '../ui/SortControl';

const SERVER_DEFAULT_LIMIT = 50;
const LIBRARY_SIZE = 60;

function servePage<TGroup>(rows: readonly TGroup[], { limit, offset = 0 }: LibraryQuery) {
  const items = rows.slice(offset, offset + (limit ?? SERVER_DEFAULT_LIMIT));
  return Promise.resolve({ items, total: items.length });
}

const albums: AlbumGroup[] = Array.from({ length: LIBRARY_SIZE }, (_, i) => ({
  key: `al${i}`,
  album: `Album ${i}`,
  artist: 'Radiohead',
  artwork_url: null,
  year: null,
  track_count: 1,
  most_recent_added_at: '2026-01-01T00:00:00Z',
}));

const artists: ArtistGroup[] = Array.from({ length: LIBRARY_SIZE }, (_, i) => ({
  key: `ar${i}`,
  artist: `Artist ${i}`,
  artwork_url: null,
  track_count: 1,
  most_recent_added_at: '2026-01-01T00:00:00Z',
}));

const mockGetLibraryAlbums = jest.fn((query: LibraryQuery) => servePage(albums, query));
const mockGetLibraryArtists = jest.fn((query: LibraryQuery) => servePage(artists, query));

jest.mock('@shared/api-client/library', () => ({
  getLibraryAlbums: (query: LibraryQuery) => mockGetLibraryAlbums(query),
  getLibraryArtists: (query: LibraryQuery) => mockGetLibraryArtists(query),
}));

const noop = () => undefined;

function AlbumsScreen(): ReactElement {
  const view = useAlbumsView({ query: '', sort: 'recent', isActive: true, onAlbumPress: noop });
  return (
    <>
      <SortControl
        count={view.count}
        noun={view.noun}
        sortKey="recent"
        options={view.options}
        onSortChange={noop}
      />
      {view.content}
    </>
  );
}

function ArtistsScreen(): ReactElement {
  const view = useArtistsView({ query: '', sort: 'az', isActive: true, onArtistPress: noop });
  return (
    <>
      <SortControl
        count={view.count}
        noun={view.noun}
        sortKey="az"
        options={view.options}
        onSortChange={noop}
      />
      {view.content}
    </>
  );
}

let client: QueryClient;
function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function scrollToEnd(gridTestID: string): void {
  fireEvent(screen.getByTestId(gridTestID), 'endReached');
}

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  mockGetLibraryAlbums.mockClear();
  mockGetLibraryArtists.mockClear();
});

afterEach(() => client.clear());

describe('a library grid holding more rows than one page', () => {
  it('shows every album once the grid is scrolled to its end', async () => {
    render(<AlbumsScreen />, { wrapper });
    await waitFor(() => expect(screen.getByText(`${SERVER_DEFAULT_LIMIT} albums`)).toBeTruthy());

    scrollToEnd('library-albums-grid');

    await waitFor(() => expect(screen.getByText(`${LIBRARY_SIZE} albums`)).toBeTruthy());
  });

  it('shows every artist once the grid is scrolled to its end', async () => {
    render(<ArtistsScreen />, { wrapper });
    await waitFor(() => expect(screen.getByText(`${SERVER_DEFAULT_LIMIT} artists`)).toBeTruthy());

    scrollToEnd('library-artists-grid');

    await waitFor(() => expect(screen.getByText(`${LIBRARY_SIZE} artists`)).toBeTruthy());
  });

  // A virtualized list only reaches its end after every cell has been laid out, which no
  // test renderer does, so the grid's end-of-list callback is checked where it is handed
  // over instead: to the list itself, the boundary between this feature and React Native.
  it.each([
    ['albums', () => render(<AlbumsScreen />, { wrapper })],
    ['artists', () => render(<ArtistsScreen />, { wrapper })],
  ])('hands the %s list its own end-of-list callback', async (noun, renderScreen) => {
    renderScreen();
    await waitFor(() => expect(screen.getByText(`${SERVER_DEFAULT_LIMIT} ${noun}`)).toBeTruthy());

    const list = screen.UNSAFE_getByType(FlatList);

    expect(list.props.onEndReached).toBeInstanceOf(Function);
  });

  it('stops asking for albums once a page comes back short of a full one', async () => {
    render(<AlbumsScreen />, { wrapper });
    await waitFor(() => expect(screen.getByText(`${SERVER_DEFAULT_LIMIT} albums`)).toBeTruthy());
    scrollToEnd('library-albums-grid');
    await waitFor(() => expect(screen.getByText(`${LIBRARY_SIZE} albums`)).toBeTruthy());

    scrollToEnd('library-albums-grid');
    scrollToEnd('library-albums-grid');

    expect(mockGetLibraryAlbums).toHaveBeenCalledTimes(2);
  });
});
