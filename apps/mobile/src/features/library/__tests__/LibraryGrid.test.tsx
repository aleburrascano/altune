import { fireEvent, render, screen } from '@testing-library/react-native';

import type { ListRefresh } from '../refresh';
import { AlbumsGrid } from '../ui/AlbumsGrid';
import { ArtistsGrid } from '../ui/ArtistsGrid';
import { PlaylistsGrid } from '../ui/PlaylistsGrid';

function idleRefresh(): ListRefresh {
  return { onRefresh: jest.fn(), refreshing: false };
}

describe('library list shells — empty state', () => {
  it('shows the albums empty label when there are no albums', () => {
    render(
      <AlbumsGrid
        albums={[]}
        emptyLabel="No albums yet"
        refresh={idleRefresh()}
        onAlbumPress={jest.fn()}
      />,
    );

    expect(screen.getByText('No albums yet')).toBeTruthy();
  });

  it('shows the artists empty label when there are no artists', () => {
    render(
      <ArtistsGrid
        artists={[]}
        emptyLabel="No artists yet"
        refresh={idleRefresh()}
        onArtistPress={jest.fn()}
      />,
    );

    expect(screen.getByText('No artists yet')).toBeTruthy();
  });

  it('offers only the create cell — no empty message — when there are no playlists', () => {
    render(
      <PlaylistsGrid
        playlists={[]}
        refresh={idleRefresh()}
        onPlaylistPress={jest.fn()}
        onCreatePress={jest.fn()}
      />,
    );

    expect(screen.getByTestId('library-create-playlist')).toBeTruthy();
    expect(screen.queryByText(/^No /)).toBeNull();
  });
});

describe('library list shells — pull to refresh', () => {
  it.each([
    [
      'albums',
      (refresh: ListRefresh) =>
        render(
          <AlbumsGrid
            albums={[]}
            emptyLabel="No albums yet"
            refresh={refresh}
            onAlbumPress={jest.fn()}
          />,
        ),
    ],
    [
      'artists',
      (refresh: ListRefresh) =>
        render(
          <ArtistsGrid
            artists={[]}
            emptyLabel="No artists yet"
            refresh={refresh}
            onArtistPress={jest.fn()}
          />,
        ),
    ],
    [
      'playlists',
      (refresh: ListRefresh) =>
        render(
          <PlaylistsGrid
            playlists={[]}
            refresh={refresh}
            onPlaylistPress={jest.fn()}
            onCreatePress={jest.fn()}
          />,
        ),
    ],
  ])('asks the %s list to refresh when the user pulls it down', (_name, renderList) => {
    const refresh = idleRefresh();
    renderList(refresh);

    fireEvent(screen.UNSAFE_getByProps({ refreshing: false }), 'refresh');

    expect(refresh.onRefresh).toHaveBeenCalledTimes(1);
  });
});
