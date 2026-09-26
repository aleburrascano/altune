import { fireEvent, render, screen } from '@testing-library/react-native';

import { asPlaylistId } from '@shared/api-client/ids';
import type { PlaylistResponse } from '@shared/api-client/types';

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
});
