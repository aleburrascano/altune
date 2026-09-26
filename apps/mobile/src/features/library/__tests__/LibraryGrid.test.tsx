import { fireEvent, render, screen } from '@testing-library/react-native';
import { FlatList } from 'react-native';

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

const { Platform } = require('react-native');

let mockWideGridWindowWidth = 390;

jest.mock('react-native/Libraries/Utilities/useWindowDimensions', () => ({
  __esModule: true,
  default: () => ({ width: mockWideGridWindowWidth, height: 800, scale: 2, fontScale: 1 }),
}));

describe('library grids — wide web layout', () => {
  const originalOS = Platform.OS;

  beforeEach(() => {
    Platform.OS = 'web';
    mockWideGridWindowWidth = 1440;
  });

  afterEach(() => {
    Platform.OS = originalOS;
    mockWideGridWindowWidth = 390;
  });

  it('gives the albums grid at least 5 columns at 1440px', () => {
    render(
      <AlbumsGrid
        albums={[]}
        emptyLabel="No albums yet"
        refresh={idleRefresh()}
        onAlbumPress={jest.fn()}
      />,
    );

    const grid = screen.UNSAFE_getByType(FlatList);
    expect((grid.props.numColumns as number) >= 5).toBe(true);
  });

  it('gives the playlists grid at least 5 columns at 1440px', () => {
    render(
      <PlaylistsGrid
        playlists={[]}
        refresh={idleRefresh()}
        onPlaylistPress={jest.fn()}
        onCreatePress={jest.fn()}
      />,
    );

    const grid = screen.UNSAFE_getByType(FlatList);
    expect((grid.props.numColumns as number) >= 5).toBe(true);
  });

  it('widens the gap between albums-grid columns at a wide width', () => {
    render(
      <AlbumsGrid
        albums={[]}
        emptyLabel="No albums yet"
        refresh={idleRefresh()}
        onAlbumPress={jest.fn()}
      />,
    );

    const grid = screen.UNSAFE_getByType(FlatList);
    expect(grid.props.columnWrapperStyle).toEqual(expect.objectContaining({ gap: expect.any(Number) }));
    const compactGap = (grid.props.columnWrapperStyle as { gap: number }).gap;

    Platform.OS = 'ios';
    screen.unmount();
    render(
      <AlbumsGrid
        albums={[]}
        emptyLabel="No albums yet"
        refresh={idleRefresh()}
        onAlbumPress={jest.fn()}
      />,
    );
    const compactGrid = screen.UNSAFE_getByType(FlatList);
    const nativeGap = (compactGrid.props.columnWrapperStyle as { gap: number }).gap;

    expect(compactGap).toBeGreaterThan(nativeGap);
  });
});
