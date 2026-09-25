// #1708: the playlists grid asked for the collection in one request — no limit sent, and
// a server that had no default clamp either — so one response grew with the number of
// playlists a user owned.
//
// The fake server below serves everything when the caller names no limit, the way the old
// one did. That is deliberate: what these assertions hold is the client's own bound, which
// must not depend on the server having a default page of its own.
//
// The count beside the sort control is what the assertions read: a virtualized grid renders
// only its window, so the cells on screen count the window, never the collection.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import type { ReactElement, ReactNode } from 'react';

import { asPlaylistId } from '@shared/api-client/ids';
import type { PlaylistPage } from '@shared/api-client/playlists';
import type { ListPlaylistsResponse, PlaylistResponse } from '@shared/api-client/types';

import { GROUP_PAGE_SIZE } from '../groupPaging';
import { usePlaylistActions } from '../hooks/usePlaylistActions';
import { usePlaylistsView } from '../hooks/usePlaylistsView';
import { SortControl } from '../ui/SortControl';

const PLAYLIST_COUNT = 3000;

const playlists: PlaylistResponse[] = Array.from({ length: PLAYLIST_COUNT }, (_unused, i) => ({
  id: asPlaylistId(`pl-${i}`),
  name: `Playlist ${i}`,
  track_count: 0,
  preview_artwork_urls: [],
  created_at: new Date(Date.UTC(2026, 0, 1) - i * 1000).toISOString(),
  updated_at: new Date(Date.UTC(2026, 0, 1) - i * 1000).toISOString(),
}));

const servedPlaylistCounts: number[] = [];

function servePage({ limit, offset = 0 }: PlaylistPage = {}): Promise<ListPlaylistsResponse> {
  const items = playlists.slice(offset, offset + (limit ?? PLAYLIST_COUNT));
  servedPlaylistCounts.push(items.length);
  return Promise.resolve({ items, total: items.length });
}

const mockGetPlaylists = jest.fn(servePage);

jest.mock('@shared/api-client/playlists', () => ({
  getPlaylists: (page?: PlaylistPage) => mockGetPlaylists(page),
}));

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
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function scrollToEnd(): void {
  fireEvent(screen.getByTestId('library-playlists-grid'), 'endReached');
}

function showsPlaylistCount(count: number): Promise<void> {
  return waitFor(() => expect(screen.getByText(`${count} playlists`)).toBeTruthy());
}

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  mockGetPlaylists.mockClear();
  servedPlaylistCounts.length = 0;
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

    expect(mockGetPlaylists.mock.calls.map(([page]) => page?.offset)).toEqual([
      0,
      GROUP_PAGE_SIZE,
    ]);
  });
});

describe('a library whose playlists all fit in one page', () => {
  it('stops asking once a page comes back short of a full one', async () => {
    mockGetPlaylists.mockImplementation(({ offset = 0 } = {}) =>
      Promise.resolve({ items: playlists.slice(offset, 10), total: 10 }),
    );
    render(<PlaylistsScreen />, { wrapper });
    await showsPlaylistCount(10);

    scrollToEnd();
    scrollToEnd();

    expect(mockGetPlaylists).toHaveBeenCalledTimes(1);
  });
});
