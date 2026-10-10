import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react-native';
import type { ReactElement, ReactNode } from 'react';
import { asPlaylistId } from '@shared/api-client/ids';
import type { PlaylistDetailResponse } from '@shared/api-client/playlistResponse';
import { createMemoryLibraryPort } from '@features/library/__tests__/memoryLibraryPort';
import { LibraryPortProvider } from '@features/library/ports/LibraryPortProvider';
import type { LibraryPort } from '@features/library/ports/libraryPort';
import { GROUP_PAGE_SIZE } from '@features/library/domain/listPaging';
import { usePlaylistActions } from '@features/library/hooks/usePlaylistActions';
import { usePlaylistsView } from '@features/library/hooks/usePlaylistsView';
import { SortControl } from '@features/library/ui/SortControl';
import { warmUpFirstRender } from '../../../../jest/warmUpFirstRender';

const PLAYLIST_COUNT = 20;
const FITS_IN_ONE_PAGE = 2;
let mockGroupPageSize = 3;

const playlists: PlaylistDetailResponse[] = Array.from(
  { length: PLAYLIST_COUNT },
  (_unused, i) => ({
    id: asPlaylistId(`pl-${i}`),
    name: `Playlist ${i}`,
    track_count: 0,
    preview_artwork_urls: [],
    created_at: new Date(Date.UTC(2026, 0, 1) - i * 1000).toISOString(),
    updated_at: new Date(Date.UTC(2026, 0, 1) - i * 1000).toISOString(),
    total_duration_seconds: 0,
    tracks: [],
  }),
);

const servedPlaylistCounts: number[] = [];

function recordingPort(seeded: PlaylistDetailResponse[]) {
  const memory = createMemoryLibraryPort({ playlists: seeded });
  async function getPlaylists(
    ...[page, signal]: Parameters<LibraryPort['getPlaylists']>
  ): ReturnType<LibraryPort['getPlaylists']> {
    const served = await memory.getPlaylists(page, signal);
    servedPlaylistCounts.push(served.items.length);
    return served;
  }
  return { ...memory, getPlaylists: jest.fn(getPlaylists) };
}

let port = recordingPort(playlists);

jest.mock('../domain/listPaging', () => {
  const actual = jest.requireActual('../domain/listPaging');
  return {
    __esModule: true,
    get GROUP_PAGE_SIZE() {
      return mockGroupPageSize;
    },
    nextGroupPageOffset: (pageLength: number, pageOffset: number) =>
      actual.nextGroupPageOffset(pageLength, pageOffset, mockGroupPageSize),
    pagedListControls: actual.pagedListControls,
  };
});

const noop = () => undefined;

function PlaylistsScreen(): ReactElement {
  const pl = usePlaylistActions();
  const { view } = usePlaylistsView({ pl, sort: 'recent', onPlaylistPress: noop });
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
function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={client}>
      <LibraryPortProvider port={port}>{children}</LibraryPortProvider>
    </QueryClientProvider>
  );
}

function scrollToEnd(): void {
  fireEvent(screen.getByTestId('library-playlists-grid'), 'endReached');
}

function showsPlaylistCount(count: number): Promise<void> {
  return waitFor(() => expect(screen.getByText(`${count} playlists`)).toBeTruthy());
}

function WarmUpPlaylistsScreen(): ReactElement {
  const pl = usePlaylistActions();
  return usePlaylistsView({ pl, sort: 'recent', onPlaylistPress: noop }).view
    .content as ReactElement;
}

warmUpFirstRender(async () => {
  const warmUpClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const warmUpPort = recordingPort(playlists);
  render(
    <QueryClientProvider client={warmUpClient}>
      <LibraryPortProvider port={warmUpPort}>
        <WarmUpPlaylistsScreen />
      </LibraryPortProvider>
    </QueryClientProvider>,
  );
  await waitFor(() => expect(warmUpPort.getPlaylists).toHaveBeenCalled());
});

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  servedPlaylistCounts.length = 0;
  port = recordingPort(playlists);
});

afterEach(() => client.clear());

describe('a library holding far more playlists than one page', () => {
  it('never takes more than a page of them in one response', async () => {
    render(<PlaylistsScreen />, { wrapper });

    await showsPlaylistCount(GROUP_PAGE_SIZE);

    expect(Math.max(...servedPlaylistCounts)).toBe(GROUP_PAGE_SIZE);
  });

  it('shows the next page of playlists once the grid is scrolled to its end', async () => {
    render(<PlaylistsScreen />, { wrapper });
    await showsPlaylistCount(GROUP_PAGE_SIZE);

    scrollToEnd();

    await showsPlaylistCount(GROUP_PAGE_SIZE * 2);
  });

  it('asks for each page once, from where the last one ended', async () => {
    render(<PlaylistsScreen />, { wrapper });
    await showsPlaylistCount(GROUP_PAGE_SIZE);

    scrollToEnd();
    await showsPlaylistCount(GROUP_PAGE_SIZE * 2);

    expect(port.getPlaylists.mock.calls.map(([page]) => page?.offset)).toEqual([
      0,
      GROUP_PAGE_SIZE,
    ]);
  });
});

describe('a library whose playlists all fit in one page', () => {
  it('stops asking once a page comes back short of a full one', async () => {
    port = recordingPort(playlists.slice(0, FITS_IN_ONE_PAGE));
    render(<PlaylistsScreen />, { wrapper });
    await showsPlaylistCount(FITS_IN_ONE_PAGE);

    scrollToEnd();
    scrollToEnd();

    expect(port.getPlaylists).toHaveBeenCalledTimes(1);
  });
});

describe('sorting playlists A to Z', () => {
  function viewWith(sort: 'az' | 'recent') {
    return renderHook(
      () => {
        const pl = usePlaylistActions();
        return usePlaylistsView({ pl, sort, onPlaylistPress: noop }).playlists;
      },
      { wrapper },
    );
  }

  it('loads every remaining page and orders all of them A to Z', async () => {
    const { result } = viewWith('az');

    await waitFor(() => expect(result.current).toHaveLength(PLAYLIST_COUNT));

    const names = result.current.map((p) => p.name);
    expect(names).toEqual([...names].sort((a, b) => a.localeCompare(b)));
  });

  it('fetches no extra pages for the recent sort', async () => {
    const { result } = viewWith('recent');

    await waitFor(() => expect(result.current).toHaveLength(GROUP_PAGE_SIZE));

    expect(port.getPlaylists).toHaveBeenCalledTimes(1);
  });
});
