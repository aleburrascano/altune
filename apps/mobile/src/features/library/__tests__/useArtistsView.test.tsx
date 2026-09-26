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
import { FlatList, View } from 'react-native';

import type { ArtistGroup, LibraryQuery } from '@shared/api-client/library';

import type { ActiveView } from '../activeView';
import { useArtistsView } from '../hooks/useArtistsView';
import { SortControl } from '../ui/SortControl';

const mockGetLibraryArtists = jest.fn();

jest.mock('@shared/api-client/library', () => ({
  getLibraryArtists: (query: LibraryQuery) => mockGetLibraryArtists(query),
}));

const noop = () => undefined;

describe('a library grid holding more rows than one page', () => {
  const SERVER_DEFAULT_LIMIT = 50;
  const LIBRARY_SIZE = 60;

  function servePage<TGroup>(rows: readonly TGroup[], { limit, offset = 0 }: LibraryQuery) {
    const items = rows.slice(offset, offset + (limit ?? SERVER_DEFAULT_LIMIT));
    return Promise.resolve({ items, total: items.length });
  }

  const artists: ArtistGroup[] = Array.from({ length: LIBRARY_SIZE }, (_, i) => ({
    key: `ar${i}`,
    artist: `Artist ${i}`,
    artwork_url: null,
    track_count: 1,
    most_recent_added_at: '2026-01-01T00:00:00Z',
  }));

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
    mockGetLibraryArtists.mockReset();
    mockGetLibraryArtists.mockImplementation((query: LibraryQuery) => servePage(artists, query));
  });

  afterEach(() => client.clear());

  it('shows every artist once the grid is scrolled to its end', async () => {
    render(<ArtistsScreen />, { wrapper });
    await waitFor(() => expect(screen.getByText(`${SERVER_DEFAULT_LIMIT} artists`)).toBeTruthy());

    scrollToEnd('library-artists-grid');

    await waitFor(() => expect(screen.getByText(`${LIBRARY_SIZE} artists`)).toBeTruthy());
  });

  // A virtualized list only reaches its end after every cell has been laid out, which no
  // test renderer does, so the grid's end-of-list callback is checked where it is handed
  // over instead: to the list itself, the boundary between this feature and React Native.
  it.each([['artists', () => render(<ArtistsScreen />, { wrapper })]])(
    'hands the %s list its own end-of-list callback',
    async (noun, renderScreen) => {
      renderScreen();
      await waitFor(() => expect(screen.getByText(`${SERVER_DEFAULT_LIMIT} ${noun}`)).toBeTruthy());

      const list = screen.UNSAFE_getByType(FlatList);

      expect(list.props.onEndReached).toBeInstanceOf(Function);
    },
  );
});

describe('a library list whose next page fails to load', () => {
  const PAGE = 50;
  const RETRY = 'library-load-more-retry';

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
    mockGetLibraryArtists.mockReset();
  });

  afterEach(() => client.clear());

  const failure = () => Promise.reject(new Error('page 2 down'));

  it.each([
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
});
