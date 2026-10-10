import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react-native';
import type { ReactNode } from 'react';
import { StyleSheet } from 'react-native';

import { asPlaylistId } from '@shared/api-client/ids';
import type { PlaylistDetailResponse } from '@shared/api-client/playlistResponse';
import { Sidebar } from '@shared/ui/navigation/Sidebar';

import { createMemoryLibraryPort } from '@features/library/__tests__/memoryLibraryPort';
import { LibraryPortProvider } from '@features/library/ports/LibraryPortProvider';
import type { LibraryPort } from '@features/library/ports/libraryPort';

import { SidebarPlaylists } from '../ui/SidebarPlaylists';

const mockPush = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush }),
}));

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
      <LibraryPortProvider port={port}>{children}</LibraryPortProvider>
    </QueryClientProvider>
  );
}

function renderSidebar(): void {
  render(<SidebarPlaylists />, { wrapper });
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
  mockPush.mockClear();
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

    expect(mockPush).toHaveBeenCalledWith('/library/playlist/42');
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
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
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
});
