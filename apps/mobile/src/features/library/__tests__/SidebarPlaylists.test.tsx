import { fireEvent, render, screen } from '@testing-library/react-native';

import { asPlaylistId } from '@shared/api-client/ids';
import type { PlaylistResponse } from '@shared/api-client/types';
import { darkTheme } from '@shared/ui';

import { SidebarPlaylists } from '../ui/SidebarPlaylists';

const mockPush = jest.fn();
const mockUsePlaylistActions = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush }),
}));

jest.mock('../hooks/usePlaylistActions', () => ({
  usePlaylistActions: () => mockUsePlaylistActions(),
}));

function playlist(id: string, name: string): PlaylistResponse {
  return {
    id: asPlaylistId(id),
    name,
    track_count: 3,
    preview_artwork_urls: [],
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
  };
}

afterEach(() => {
  mockPush.mockClear();
});

describe('SidebarPlaylists', () => {
  it('lists each playlist by name', () => {
    mockUsePlaylistActions.mockReturnValue({
      playlists: [playlist('1', 'Late Night'), playlist('2', 'Road Trip')],
    });

    render(<SidebarPlaylists />);

    expect(screen.getByText('Late Night')).toBeTruthy();
    expect(screen.getByText('Road Trip')).toBeTruthy();
  });

  it('navigates to the playlist detail route when pressed', () => {
    mockUsePlaylistActions.mockReturnValue({ playlists: [playlist('42', 'Late Night')] });

    render(<SidebarPlaylists />);
    fireEvent.press(screen.getByTestId('sidebar-playlist-42'));

    expect(mockPush).toHaveBeenCalledWith('/library/playlist/42');
  });

  it('shows an empty hint when there are no playlists', () => {
    mockUsePlaylistActions.mockReturnValue({ playlists: [] });

    render(<SidebarPlaylists />);

    expect(screen.getByTestId('sidebar-playlists-empty')).toBeTruthy();
  });

  it('shows a quiet loading placeholder while fetching, not the empty hint', () => {
    mockUsePlaylistActions.mockReturnValue({ playlists: [], isLoadingPlaylists: true });

    render(<SidebarPlaylists />);

    expect(screen.getByTestId('sidebar-playlists-loading')).toBeTruthy();
    expect(screen.queryByTestId('sidebar-playlists-empty')).toBeNull();
  });

  it('shows an error message with a retry after a failed fetch, not the empty hint', () => {
    const refetchPlaylists = jest.fn();
    mockUsePlaylistActions.mockReturnValue({
      playlists: [],
      isLoadingPlaylists: false,
      playlistsError: new Error('network down'),
      refetchPlaylists,
    });

    render(<SidebarPlaylists />);

    expect(screen.getByTestId('sidebar-playlists-error')).toBeTruthy();
    expect(screen.queryByTestId('sidebar-playlists-empty')).toBeNull();

    fireEvent.press(screen.getByTestId('sidebar-playlists-retry'));

    expect(refetchPlaylists).toHaveBeenCalledTimes(1);
  });

  it('gives a playlist row the same hover and focus ring as the top-level sidebar items', () => {
    mockUsePlaylistActions.mockReturnValue({ playlists: [playlist('1', 'Late Night')] });

    render(<SidebarPlaylists />);
    const row = screen.UNSAFE_getByProps({ testID: 'sidebar-playlist-1' });
    const resolveStyle = row.props.style as (state: {
      hovered?: boolean;
      focused?: boolean;
      pressed?: boolean;
    }) => unknown[];

    expect(resolveStyle({ hovered: true, focused: false, pressed: false })).toEqual(
      expect.arrayContaining([{ backgroundColor: darkTheme.color.surface2 }]),
    );
    expect(resolveStyle({ hovered: false, focused: true, pressed: false })).toEqual(
      expect.arrayContaining([{ borderColor: darkTheme.color.accent }]),
    );
    expect(resolveStyle({ hovered: false, focused: false, pressed: false })).toEqual(
      expect.arrayContaining([{ borderColor: 'transparent' }]),
    );
  });
});
