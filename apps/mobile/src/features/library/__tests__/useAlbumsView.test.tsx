import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import type { ReactElement, ReactNode } from 'react';
import { FlatList, View } from 'react-native';

import type { AlbumGroup } from '@shared/api-client/library';

import {
  createMemoryLibraryPort,
  type MemoryLibrarySeed,
} from '@features/library/__tests__/memoryLibraryPort';
import { LibraryPortProvider } from '@features/library/ports/LibraryPortProvider';

import type { ActiveView } from '@features/library/domain/activeView';
import { useAlbumsView } from '../hooks/useAlbumsView';
import { SortControl } from '../ui/SortControl';
import { warmUpFirstRender } from '../../../../jest/warmUpFirstRender';

let mockGroupPageSize = 50;

jest.mock('@features/library/domain/groupPaging', () => {
  const actual = jest.requireActual('@features/library/domain/groupPaging');
  return {
    __esModule: true,
    get GROUP_PAGE_SIZE() {
      return mockGroupPageSize;
    },
    nextGroupPageOffset: (pageLength: number, pageOffset: number) =>
      actual.nextGroupPageOffset(pageLength, pageOffset, mockGroupPageSize),
  };
});

const noop = () => undefined;

function spiedPort(seed: MemoryLibrarySeed) {
  const memory = createMemoryLibraryPort(seed);
  return { ...memory, getLibraryAlbums: jest.fn(memory.getLibraryAlbums) };
}

function WarmUpAlbumsScreen() {
  const view = useAlbumsView({ query: '', sort: 'recent', isActive: true, onAlbumPress: noop });
  return view.content;
}

warmUpFirstRender(async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const port = spiedPort({});
  render(
    <QueryClientProvider client={client}>
      <LibraryPortProvider port={port}>
        <WarmUpAlbumsScreen />
      </LibraryPortProvider>
    </QueryClientProvider>,
  );
  await waitFor(() => expect(port.getLibraryAlbums).toHaveBeenCalled());
});

describe('a library grid holding more rows than one page', () => {
  const SERVER_DEFAULT_LIMIT = 50;
  const LIBRARY_SIZE = 60;

  const albums: AlbumGroup[] = Array.from({ length: LIBRARY_SIZE }, (_, i) => ({
    key: `al${i}`,
    album: `Album ${i}`,
    artist: 'Radiohead',
    artwork_url: null,
    year: null,
    track_count: 1,
    most_recent_added_at: '2026-01-01T00:00:00Z',
  }));

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

  let client: QueryClient;
  let port: ReturnType<typeof spiedPort>;
  function wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <LibraryPortProvider port={port}>{children}</LibraryPortProvider>
      </QueryClientProvider>
    );
  }

  function scrollToEnd(gridTestID: string): void {
    fireEvent(screen.getByTestId(gridTestID), 'endReached');
  }

  beforeEach(() => {
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    port = spiedPort({ albums });
  });

  afterEach(() => client.clear());

  it('shows every album once the grid is scrolled to its end', async () => {
    render(<AlbumsScreen />, { wrapper });
    await waitFor(() => expect(screen.getByText(`${SERVER_DEFAULT_LIMIT} albums`)).toBeTruthy());

    scrollToEnd('library-albums-grid');

    await waitFor(() => expect(screen.getByText(`${LIBRARY_SIZE} albums`)).toBeTruthy());
  });

  it.each([['albums', () => render(<AlbumsScreen />, { wrapper })]])(
    'hands the %s list its own end-of-list callback',
    async (noun, renderScreen) => {
      renderScreen();
      await waitFor(() => expect(screen.getByText(`${SERVER_DEFAULT_LIMIT} ${noun}`)).toBeTruthy());

      const list = screen.UNSAFE_getByType(FlatList);

      expect(list.props.onEndReached).toBeInstanceOf(Function);
    },
  );

  it('stops asking for albums once a page comes back short of a full one', async () => {
    render(<AlbumsScreen />, { wrapper });
    await waitFor(() => expect(screen.getByText(`${SERVER_DEFAULT_LIMIT} albums`)).toBeTruthy());
    scrollToEnd('library-albums-grid');
    await waitFor(() => expect(screen.getByText(`${LIBRARY_SIZE} albums`)).toBeTruthy());

    scrollToEnd('library-albums-grid');
    scrollToEnd('library-albums-grid');

    expect(port.getLibraryAlbums).toHaveBeenCalledTimes(2);
  });
});

describe('a library list whose next page fails to load', () => {
  const PAGE = 5;
  const RETRY = 'library-load-more-retry';

  const album = (i: number) => ({
    key: `al${i}`,
    album: `Album ${i}`,
    artist: 'Radiohead',
    artwork_url: null,
    year: null,
    track_count: 1,
    most_recent_added_at: '2026-01-01T00:00:00Z',
  });

  const rows = <T,>(count: number, make: (i: number) => T): T[] =>
    Array.from({ length: count }, (_, i) => make(i));

  const libraryRows = rows(PAGE * 2 + 1, album);

  function Screen({ view }: { view: ActiveView }): ReactElement {
    return view.error ? <View testID="library-error" /> : <>{view.content}</>;
  }

  function AlbumsScreen(): ReactElement {
    const view = useAlbumsView({ query: '', sort: 'recent', isActive: true, onAlbumPress: noop });
    return <Screen view={view} />;
  }

  let client: QueryClient;
  let port: ReturnType<typeof spiedPort>;
  function wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <LibraryPortProvider port={port}>{children}</LibraryPortProvider>
      </QueryClientProvider>
    );
  }

  beforeEach(() => {
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    port = spiedPort({ albums: libraryRows });
    mockGroupPageSize = PAGE;
  });

  afterEach(() => {
    client.clear();
    mockGroupPageSize = 50;
  });

  const failure = () => Promise.reject(new Error('page 2 down'));

  it.each([
    [
      'albums',
      'library-albums-grid',
      'Album 0',
      AlbumsScreen,
      () => {
        const { getLibraryAlbums: servePage } = createMemoryLibraryPort({ albums: libraryRows });
        port.getLibraryAlbums.mockImplementationOnce(servePage);
        port.getLibraryAlbums.mockImplementationOnce(failure);
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
    port.getLibraryAlbums.mockImplementation(failure);
    render(<AlbumsScreen />, { wrapper });

    await waitFor(() => expect(screen.getByTestId('library-error')).toBeTruthy());
  });
});
