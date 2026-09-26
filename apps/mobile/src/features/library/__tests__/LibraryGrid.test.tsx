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

const { asPlaylistId: probeAsPlaylistId } = require('@shared/api-client/ids');
const { PlaylistCover: ProbePlaylistCover } = require('../ui/PlaylistCover');

const probePlaylist = {
  id: probeAsPlaylistId('pl-probe'),
  name: 'Road Trip',
  track_count: 12,
  preview_artwork_urls: [],
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
};

function probeAlbums(paging?: unknown) {
  return render(
    <AlbumsGrid
      albums={[]}
      emptyLabel="No albums yet"
      refresh={idleRefresh()}
      onAlbumPress={jest.fn()}
      {...(paging ? { paging: paging as never } : {})}
    />,
  );
}

function probeArtists() {
  return render(
    <ArtistsGrid artists={[]} emptyLabel="No artists yet" refresh={idleRefresh()} onArtistPress={jest.fn()} />,
  );
}

function probePlaylists(paging?: unknown) {
  return render(
    <PlaylistsGrid
      playlists={[probePlaylist]}
      refresh={idleRefresh()}
      onPlaylistPress={jest.fn()}
      onCreatePress={jest.fn()}
      {...(paging ? { paging: paging as never } : {})}
    />,
  );
}

function measureGrid(width: number) {
  fireEvent(screen.UNSAFE_getByType(FlatList), 'layout', {
    nativeEvent: { layout: { x: 0, y: 0, width, height: 800 } },
  });
}

const gridColumnCount = () => screen.UNSAFE_getByType(FlatList).props.numColumns as number;
const playlistCoverSize = () => screen.UNSAFE_getByType(ProbePlaylistCover).props.size as number;

function probePaging(overrides: Partial<{ isFetchingNextPage: boolean; nextPageFailed: boolean }> = {}) {
  return {
    onEndReached: jest.fn(),
    isFetchingNextPage: false,
    nextPageFailed: false,
    onRetryNextPage: jest.fn(),
    ...overrides,
  };
}

describe('library grids — wide web, measured inside the app shell (#2842)', () => {
  const originalOS = Platform.OS;
  // 1440 window - 240 sidebar - 2 x 16 screen padding.
  const MEASURED_AT_1440 = 1168;

  beforeEach(() => {
    Platform.OS = 'web';
    mockWideGridWindowWidth = 1440;
  });

  afterEach(() => {
    Platform.OS = originalOS;
    mockWideGridWindowWidth = 390;
  });

  it('gives the artists grid at least 5 columns at 1440px', () => {
    probeArtists();

    expect(gridColumnCount()).toBeGreaterThanOrEqual(5);
  });

  it.each([
    ['albums', probeAlbums],
    ['artists', probeArtists],
    ['playlists', probePlaylists],
  ])('keeps the %s grid at 5 or more columns once it measures its real width beside the sidebar', (_name, renderGrid) => {
    renderGrid();

    measureGrid(MEASURED_AT_1440);

    expect(gridColumnCount()).toBeGreaterThanOrEqual(5);
  });

  it('fits every playlist cover of a row inside the measured width', () => {
    probePlaylists();

    measureGrid(MEASURED_AT_1440);

    expect(playlistCoverSize()).toBeGreaterThan(0);
    expect(gridColumnCount() * playlistCoverSize()).toBeLessThanOrEqual(MEASURED_AT_1440);
  });

  it('follows the measured width across a resize and back', () => {
    probePlaylists();
    measureGrid(MEASURED_AT_1440);
    const wideColumns = gridColumnCount();
    const wideCover = playlistCoverSize();

    measureGrid(400);
    const narrowColumns = gridColumnCount();

    measureGrid(MEASURED_AT_1440);

    expect(narrowColumns).toBeLessThan(wideColumns);
    expect(gridColumnCount()).toBe(wideColumns);
    expect(playlistCoverSize()).toBe(wideCover);
  });

  it('asks the wide albums grid for its next page at the end of the list', () => {
    const paging = probePaging();
    probeAlbums(paging);
    measureGrid(MEASURED_AT_1440);

    fireEvent(screen.UNSAFE_getByType(FlatList), 'endReached');

    expect(paging.onEndReached).toHaveBeenCalledTimes(1);
  });

  it('offers the load-more retry under the wide playlists grid when its next page failed', () => {
    const paging = probePaging({ nextPageFailed: true });
    probePlaylists(paging);
    measureGrid(MEASURED_AT_1440);

    fireEvent.press(screen.getByTestId('library-load-more-retry'));

    expect(paging.onRetryNextPage).toHaveBeenCalledTimes(1);
  });

  it('shows the loading-more footer under the wide albums grid while its next page loads', () => {
    probeAlbums(probePaging({ isFetchingNextPage: true }));

    expect(screen.getByText('Loading more…')).toBeTruthy();
  });

  it('keeps the tablet tier of 3 cover columns at 999px on web', () => {
    mockWideGridWindowWidth = 999;
    probeAlbums();

    expect(gridColumnCount()).toBe(3);
  });
});

describe('library grids — native and narrow web keep their pre-#2842 columns once measured', () => {
  const originalOS = Platform.OS;

  afterEach(() => {
    Platform.OS = originalOS;
    mockWideGridWindowWidth = 390;
  });

  it('keeps 3 albums columns on a 720pt native tablet whose grid measures 688 inside the padding', () => {
    Platform.OS = 'ios';
    mockWideGridWindowWidth = 720;
    probeAlbums();

    measureGrid(688);

    expect(gridColumnCount()).toBe(3);
  });

  it('keeps 221pt playlist covers on a 720pt native tablet once measured', () => {
    Platform.OS = 'ios';
    mockWideGridWindowWidth = 720;
    probePlaylists();

    measureGrid(688);

    expect(gridColumnCount()).toBe(3);
    expect(playlistCoverSize()).toBe(221);
  });

  it('keeps 4 albums columns on a 1000pt native tablet whose grid measures 968', () => {
    Platform.OS = 'android';
    mockWideGridWindowWidth = 1000;
    probeAlbums();

    measureGrid(968);

    expect(gridColumnCount()).toBe(4);
  });

  it('keeps 5 artists columns on a 720pt native tablet once measured', () => {
    Platform.OS = 'ios';
    mockWideGridWindowWidth = 720;
    probeArtists();

    measureGrid(688);

    expect(gridColumnCount()).toBe(5);
  });

  it('keeps 3 albums columns on a 720px narrow web window whose grid measures 688', () => {
    Platform.OS = 'web';
    mockWideGridWindowWidth = 720;
    probeAlbums();

    measureGrid(688);

    expect(gridColumnCount()).toBe(3);
  });
});

describe('library grids — playlist cover size at a tiny measured width', () => {
  const originalOS = Platform.OS;

  beforeEach(() => {
    Platform.OS = 'web';
    mockWideGridWindowWidth = 1440;
  });

  afterEach(() => {
    Platform.OS = originalOS;
    mockWideGridWindowWidth = 390;
  });

  it('never returns a negative playlist cover size once the grid measures a very narrow width', () => {
    probePlaylists();

    measureGrid(10);

    expect(playlistCoverSize()).toBeGreaterThanOrEqual(0);
  });

  it('ignores a zero-width layout event and keeps the window-fallback cover size', () => {
    probePlaylists();
    const fallbackSize = playlistCoverSize();

    measureGrid(0);

    expect(playlistCoverSize()).toBe(fallbackSize);
  });
});

