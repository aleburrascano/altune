import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import type { Navigator } from '@shared/navigation';
import { StyleSheet } from 'react-native';
import { asPlaylistId } from '@shared/catalog';
import type { PlaylistDetailResponse } from '@shared/catalog';
import { Sidebar, darkTheme } from '@shared/ui';
import { LibraryProvider } from '@features/library/view/LibraryProvider';
import { SidebarPlaylists } from '@features/library/view/SidebarPlaylists';
import { createMemoryLibraryPort } from '@testing/fakes/library/inMemoryLibraryPort';
import type { LibraryPort } from '@features/library/ports/ports';
import { PlaylistsPortProvider } from '@shared/playlists';
import { createMemoryPlaylistsPort } from '@testing/fakes/playlists/inMemoryPlaylistsPort';

const playlistsPort = createMemoryPlaylistsPort();
const navigator: Navigator = {
  push: jest.fn(),
  replace: jest.fn(),
  back: jest.fn(),
  canGoBack: () => false,
};
const push = jest.mocked(navigator.push);

const TRACK_COUNT = 3;

interface WrapperProps {
  children: ReactNode;
}

function playlist(id: string, name: string): PlaylistDetailResponse {
  return {
    id: asPlaylistId(id),
    name,
    track_count: TRACK_COUNT,
    preview_artwork_urls: [],
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    total_duration_seconds: 0,
    tracks: [],
  };
}

function failingOnce(port: LibraryPort): LibraryPort {
  let failed = false;
  async function getPlaylists(
    ...args: Parameters<LibraryPort['getPlaylists']>
  ): ReturnType<LibraryPort['getPlaylists']> {
    if (failed) return port.getPlaylists(...args);
    failed = true;
    throw new Error('network down');
  }
  return { ...port, getPlaylists };
}

let client: QueryClient;
let port: LibraryPort;

function wrapper({ children }: WrapperProps) {
  return (
    <QueryClientProvider client={client}>
      <PlaylistsPortProvider port={playlistsPort}>
        <LibraryProvider port={port}>{children}</LibraryProvider>
      </PlaylistsPortProvider>
    </QueryClientProvider>
  );
}

function renderSidebar(): void {
  render(<SidebarPlaylists navigator={navigator} />, { wrapper });
}

function restingStyleOf(testId: string) {
  return StyleSheet.flatten(screen.getByTestId(testId).props.style);
}

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  port = createMemoryLibraryPort();
});

afterEach(() => {
  client.clear();
  push.mockClear();
  jest.restoreAllMocks();
});

describe('SidebarPlaylists', () => {
  it('lists each playlist by name', async () => {
    port = createMemoryLibraryPort({
      playlists: [playlist('1', 'Late Night'), playlist('2', 'Road Trip')],
    });

    renderSidebar();

    expect(await screen.findByText('Late Night')).toBeTruthy();
    expect(screen.getByText('Road Trip')).toBeTruthy();
  });

  it('navigates to the playlist detail route when pressed', async () => {
    port = createMemoryLibraryPort({ playlists: [playlist('42', 'Late Night')] });

    renderSidebar();
    fireEvent.press(await screen.findByLabelText('Late Night'));

    expect(push).toHaveBeenCalledWith('/library/playlist/42');
  });

  it('shows an empty hint when there are no playlists', async () => {
    renderSidebar();

    expect(await screen.findByText('No playlists yet')).toBeTruthy();
  });

  it('shows a quiet loading placeholder while fetching, not the empty hint', async () => {
    renderSidebar();

    expect(screen.getByText('Loading…')).toBeTruthy();
    expect(screen.queryByText('No playlists yet')).toBeNull();
    await screen.findByText('No playlists yet');
  });

  it('shows an error message with a retry after a failed fetch, not the empty hint', async () => {
    port = failingOnce(createMemoryLibraryPort({ playlists: [playlist('1', 'Late Night')] }));

    renderSidebar();

    expect(await screen.findByText("Couldn't load playlists.")).toBeTruthy();
    expect(screen.queryByText('No playlists yet')).toBeNull();

    fireEvent.press(screen.getByLabelText('Retry loading playlists'));

    expect(await screen.findByText('Late Night')).toBeTruthy();
  });

  it('rests a playlist row with the same look as an inactive top-level sidebar item', async () => {
    port = createMemoryLibraryPort({ playlists: [playlist('1', 'Late Night')] });
    render(<Sidebar activeRoute="settings" onNavigate={jest.fn()} />);
    const topLevelItem = restingStyleOf('sidebar-item-discover');
    screen.unmount();

    renderSidebar();
    await screen.findByText('Late Night');

    expect(restingStyleOf('sidebar-playlist-1')).toEqual(topLevelItem);
  });

  it('fills a playlist row with the raised surface while hovered, and clears it on hover-out', async () => {
    port = createMemoryLibraryPort({ playlists: [playlist('1', 'Late Night')] });
    renderSidebar();
    const row = await screen.findByTestId('sidebar-playlist-1');

    fireEvent(row, 'hoverIn');
    expect(row).toHaveStyle({ backgroundColor: darkTheme.color.surface2 });
    expect(row).toHaveStyle({ borderColor: 'transparent' });

    fireEvent(row, 'hoverOut');
    expect(row).not.toHaveStyle({ backgroundColor: darkTheme.color.surface2 });
  });

  it('rings a focused playlist row in the accent colour, and clears it on blur', async () => {
    port = createMemoryLibraryPort({ playlists: [playlist('1', 'Late Night')] });
    renderSidebar();
    const row = await screen.findByTestId('sidebar-playlist-1');

    fireEvent(row, 'focus');
    expect(row).toHaveStyle({ borderWidth: 2, borderColor: darkTheme.color.accent });
    expect(row).not.toHaveStyle({ backgroundColor: darkTheme.color.surface2 });

    fireEvent(row, 'blur');
    expect(row).toHaveStyle({ borderColor: 'transparent' });
  });
});
