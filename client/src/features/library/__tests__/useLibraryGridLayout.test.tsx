import { fireEvent, render, screen, within } from '@testing-library/react-native';
import type { ReactTestInstance } from 'react-test-renderer';
import { Platform } from 'react-native';
import { asPlaylistId } from '@shared/apiClient/ids';
import type { AlbumGroup, ArtistGroup } from '@shared/apiClient/library';
import type { PlaylistResponse } from '@shared/apiClient/playlistResponse';
import type { ListRefresh } from '@features/library/domain/libraryView';
import { AlbumsGrid, ArtistsGrid } from '@features/library/ui/GroupGrids';
import { PlaylistsGrid } from '@features/library/ui/PlaylistsGrid';
import { setPlatformOS } from '@testing/setPlatformOS';

let mockWindowWidth = 390;

jest.mock('react-native/Libraries/Utilities/useWindowDimensions', () => ({
  __esModule: true,
  default: () => ({ width: mockWindowWidth, height: 800, scale: 2, fontScale: 1 }),
}));

function idleRefresh(): ListRefresh {
  return { onRefresh: jest.fn(), refreshing: false };
}

const playlists: PlaylistResponse[] = Array.from({ length: 11 }, (_, i) => ({
  id: asPlaylistId(`pl-${i + 1}`),
  name: `Road Trip ${i + 1}`,
  track_count: 12,
  preview_artwork_urls: [],
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}));

const albums: AlbumGroup[] = Array.from({ length: 12 }, (_, i) => ({
  key: `al-${i}`,
  album: `Album ${i}`,
  artist: 'Artist',
  artwork_url: null,
  year: null,
  track_count: 1,
  most_recent_added_at: '2026-01-01T00:00:00Z',
}));

const artists: ArtistGroup[] = Array.from({ length: 12 }, (_, i) => ({
  key: `ar-${i}`,
  artist: `Artist ${i}`,
  artwork_url: null,
  track_count: 1,
  most_recent_added_at: '2026-01-01T00:00:00Z',
}));

const ALBUM_CELL = /^library-album-/;
const ARTIST_CELL = /^library-artist-/;
const PLAYLIST_CELL = /^library-(create-playlist|playlist-)/;

function renderAlbums() {
  render(
    <AlbumsGrid
      albums={albums}
      emptyLabel="No albums yet"
      refresh={idleRefresh()}
      onAlbumPress={jest.fn()}
    />,
  );
}

function renderArtists() {
  render(
    <ArtistsGrid
      artists={artists}
      emptyLabel="No artists yet"
      refresh={idleRefresh()}
      onArtistPress={jest.fn()}
    />,
  );
}

function renderPlaylists() {
  render(
    <PlaylistsGrid
      playlists={playlists}
      refresh={idleRefresh()}
      onPlaylistPress={jest.fn()}
      onCreatePress={jest.fn()}
    />,
  );
}

type MeasuredLayout = { width: number };

function measure(gridTestId: string, { width }: MeasuredLayout) {
  fireEvent(screen.getByTestId(gridTestId), 'layout', {
    nativeEvent: { layout: { x: 0, y: 0, width, height: 800 } },
  });
}

function cellsInFirstRow(cell: RegExp): number {
  let row: ReactTestInstance | null = screen.getAllByTestId(cell)[0]!.parent;
  while (row != null && typeof row.type !== 'string') row = row.parent;
  return within(row!).getAllByTestId(cell).length;
}

function playlistCover(): ReactTestInstance {
  const cell = screen.getByTestId('library-playlist-pl-1');
  return cell.findAll((node) => node !== cell && typeof node.type === 'string')[0]!;
}

describe.each([
  { label: 'a narrow phone', width: 390, cover: 2, avatar: 3, coverSide: 173 },
  { label: 'a tablet', width: 800, cover: 3, avatar: 5, coverSide: 248 },
  { label: 'a wide screen', width: 1200, cover: 4, avatar: 6, coverSide: 283 },
])('at $label width ($width px)', (expected) => {
  beforeEach(() => {
    mockWindowWidth = expected.width;
  });

  it(`lays out ${expected.cover} albums per row`, () => {
    renderAlbums();

    expect(cellsInFirstRow(ALBUM_CELL)).toBe(expected.cover);
  });

  it(`lays out ${expected.avatar} artists per row`, () => {
    renderArtists();

    expect(cellsInFirstRow(ARTIST_CELL)).toBe(expected.avatar);
  });

  it(`lays out ${expected.cover} playlist cells per row`, () => {
    renderPlaylists();

    expect(cellsInFirstRow(PLAYLIST_CELL)).toBe(expected.cover);
  });

  it(`draws ${expected.coverSide}px playlist covers from the window width, screen padding and row gap`, () => {
    renderPlaylists();

    expect(playlistCover()).toHaveStyle({
      width: expected.coverSide,
      height: expected.coverSide,
    });
  });
});

describe('a measured content width narrower than the window overrides the window fallback', () => {
  const originalWideOS = Platform.OS;
  const expected = { columnsBeforeLayout: 5, measured: { width: 800 }, columns: 3, coverSide: 258 };

  beforeEach(() => {
    setPlatformOS('web');
    mockWindowWidth = 1440;
  });

  afterEach(() => {
    setPlatformOS(originalWideOS);
  });

  it("sizes AlbumsGrid's columns from the grid's own measured width, not the window width", () => {
    renderAlbums();
    expect(cellsInFirstRow(ALBUM_CELL)).toBe(expected.columnsBeforeLayout);

    measure('library-albums-grid', expected.measured);

    expect(cellsInFirstRow(ALBUM_CELL)).toBe(expected.columns);
  });

  it("sizes PlaylistsGrid's covers from the grid's own measured width, not the window width", () => {
    renderPlaylists();

    measure('library-playlists-grid', expected.measured);

    expect(cellsInFirstRow(PLAYLIST_CELL)).toBe(expected.columns);
    expect(playlistCover()).toHaveStyle({ width: expected.coverSide, height: expected.coverSide });
  });
});

describe('a native tablet just under the wide breakpoint keeps its column count', () => {
  const tablet = { coverColumns: 3 };
  beforeEach(() => {
    mockWindowWidth = 999;
  });

  it('gives AlbumsGrid 3 cover columns at a 999pt-wide window', () => {
    renderAlbums();

    expect(cellsInFirstRow(ALBUM_CELL)).toBe(tablet.coverColumns);
  });
});

describe('narrow web keeps its pre-#2842 grid once the grid measures itself', () => {
  const originalOS = Platform.OS;
  const narrowWeb = { columns: 2, measured: { width: 328 }, coverSide: 158 };

  beforeEach(() => {
    setPlatformOS('web');
    mockWindowWidth = 360;
  });

  afterEach(() => {
    setPlatformOS(originalOS);
    mockWindowWidth = 390;
  });

  it('gives AlbumsGrid 2 columns at 360px on web before any layout', () => {
    renderAlbums();

    expect(cellsInFirstRow(ALBUM_CELL)).toBe(narrowWeb.columns);
  });

  it('keeps 2 columns and 158px playlist covers at 360px on web once the grid measures 328px', () => {
    renderPlaylists();

    measure('library-playlists-grid', narrowWeb.measured);

    expect(cellsInFirstRow(PLAYLIST_CELL)).toBe(narrowWeb.columns);
    expect(playlistCover()).toHaveStyle({
      width: narrowWeb.coverSide,
      height: narrowWeb.coverSide,
    });
  });
});

describe.each([
  { width: 1366, coverColumns: 4, coverSide: 324 },
  { width: 1440, coverColumns: 4, coverSide: 343 },
])('a native window of $width pt matches origin/main, uncapped and uncropped', (expected) => {
  beforeEach(() => {
    mockWindowWidth = expected.width;
  });

  it(`gives AlbumsGrid ${expected.coverColumns} columns`, () => {
    renderAlbums();

    expect(cellsInFirstRow(ALBUM_CELL)).toBe(expected.coverColumns);
  });

  it(`draws ${expected.coverSide}px playlist covers from the raw window width`, () => {
    renderPlaylists();

    expect(playlistCover()).toHaveStyle({ width: expected.coverSide, height: expected.coverSide });
  });
});

describe('web below the wide-web threshold behaves like main, uncapped and uncropped', () => {
  const originalBelowWideOS = Platform.OS;
  const belowWide = { coverColumns: 3 };

  beforeEach(() => {
    setPlatformOS('web');
    mockWindowWidth = 900;
  });

  afterEach(() => {
    setPlatformOS(originalBelowWideOS);
    mockWindowWidth = 390;
  });

  it('gives AlbumsGrid 3 columns, not the wide-web 5-column tier', () => {
    renderAlbums();

    expect(cellsInFirstRow(ALBUM_CELL)).toBe(belowWide.coverColumns);
  });
});
