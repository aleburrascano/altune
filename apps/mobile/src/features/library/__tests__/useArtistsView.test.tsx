import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import type { ReactElement, ReactNode } from 'react';
import { FlatList, View } from 'react-native';

import type { ArtistGroup } from '@shared/api-client/library';

import {
  createMemoryLibraryPort,
  type MemoryLibrarySeed,
} from '@features/library/__tests__/memoryLibraryPort';
import { LibraryPortProvider } from '@features/library/ports/LibraryPortProvider';

import type { ActiveView } from '@features/library/domain/activeView';
import { useArtistsView } from '../hooks/useArtistsView';
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
  return { ...memory, getLibraryArtists: jest.fn(memory.getLibraryArtists) };
}

function WarmUpArtistsScreen() {
  const view = useArtistsView({ query: '', sort: 'az', isActive: true, onArtistPress: noop });
  return view.content;
}

warmUpFirstRender(async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const port = spiedPort({});
  render(
    <QueryClientProvider client={client}>
      <LibraryPortProvider port={port}>
        <WarmUpArtistsScreen />
      </LibraryPortProvider>
    </QueryClientProvider>,
  );
  await waitFor(() => expect(port.getLibraryArtists).toHaveBeenCalled());
});

describe('a library grid holding more rows than one page', () => {
  const SERVER_DEFAULT_LIMIT = 50;
  const LIBRARY_SIZE = 60;

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
    port = spiedPort({ artists });
  });

  afterEach(() => client.clear());

  it('shows every artist once the grid is scrolled to its end', async () => {
    render(<ArtistsScreen />, { wrapper });
    await waitFor(() => expect(screen.getByText(`${SERVER_DEFAULT_LIMIT} artists`)).toBeTruthy());

    scrollToEnd('library-artists-grid');

    await waitFor(() => expect(screen.getByText(`${LIBRARY_SIZE} artists`)).toBeTruthy());
  });

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
  const PAGE = 5;
  const RETRY = 'library-load-more-retry';

  const artist = (i: number) => ({
    key: `ar${i}`,
    artist: `Artist ${i}`,
    artwork_url: null,
    track_count: 1,
    most_recent_added_at: '2026-01-01T00:00:00Z',
  });

  const rows = <T,>(count: number, make: (i: number) => T): T[] =>
    Array.from({ length: count }, (_, i) => make(i));

  const libraryRows = rows(PAGE * 2 + 1, artist);

  function Screen({ view }: { view: ActiveView }): ReactElement {
    return view.error ? <View testID="library-error" /> : <>{view.content}</>;
  }

  function ArtistsScreen(): ReactElement {
    const view = useArtistsView({ query: '', sort: 'az', isActive: true, onArtistPress: noop });
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
    port = spiedPort({ artists: libraryRows });
    mockGroupPageSize = PAGE;
  });

  afterEach(() => {
    client.clear();
    mockGroupPageSize = 50;
  });

  const failure = () => Promise.reject(new Error('page 2 down'));

  it.each([
    [
      'artists',
      'library-artists-grid',
      'Artist 0',
      ArtistsScreen,
      () => {
        const { getLibraryArtists: servePage } = createMemoryLibraryPort({ artists: libraryRows });
        port.getLibraryArtists.mockImplementationOnce(servePage);
        port.getLibraryArtists.mockImplementationOnce(failure);
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
