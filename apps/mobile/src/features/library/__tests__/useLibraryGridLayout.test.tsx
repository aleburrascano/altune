// #2820: before the three grids share one layout seam, this pins today's behaviour —
// the column count each grid hands its FlatList, and the cover size PlaylistsGrid hands
// PlaylistCover — at three widths spanning every breakpoint in gridColumns.ts. It must stay
// green, unchanged, once the seam lands.

import { render, screen } from '@testing-library/react-native';
import { FlatList } from 'react-native';

import { spacing } from '@shared/ui';

import { asPlaylistId } from '@shared/api-client/ids';
import type { PlaylistResponse } from '@shared/api-client/types';

import { avatarColumns, cellSize, coverColumns } from '../gridColumns';
import type { ListRefresh } from '../refresh';
import { AlbumsGrid } from '../ui/AlbumsGrid';
import { ArtistsGrid } from '../ui/ArtistsGrid';
import { PlaylistCover } from '../ui/PlaylistCover';
import { PlaylistsGrid } from '../ui/PlaylistsGrid';

let mockWindowWidth = 390;

jest.mock('react-native/Libraries/Utilities/useWindowDimensions', () => ({
  __esModule: true,
  default: () => ({ width: mockWindowWidth, height: 800, scale: 2, fontScale: 1 }),
}));

function idleRefresh(): ListRefresh {
  return { onRefresh: jest.fn(), refreshing: false };
}

const playlist: PlaylistResponse = {
  id: asPlaylistId('pl-1'),
  name: 'Road Trip',
  track_count: 12,
  preview_artwork_urls: [],
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
};

describe.each([
  ['a narrow phone', 390],
  ['a tablet', 800],
  ['a wide screen', 1200],
])('at %s width (%dpx)', (_label, width) => {
  beforeEach(() => {
    mockWindowWidth = width;
  });

  it('gives AlbumsGrid the same numColumns as coverColumns(width)', () => {
    render(
      <AlbumsGrid albums={[]} emptyLabel="No albums yet" refresh={idleRefresh()} onAlbumPress={jest.fn()} />,
    );

    expect(screen.UNSAFE_getByType(FlatList).props.numColumns).toBe(coverColumns(width));
  });

  it('gives ArtistsGrid the same numColumns as avatarColumns(width)', () => {
    render(
      <ArtistsGrid
        artists={[]}
        emptyLabel="No artists yet"
        refresh={idleRefresh()}
        onArtistPress={jest.fn()}
      />,
    );

    expect(screen.UNSAFE_getByType(FlatList).props.numColumns).toBe(avatarColumns(width));
  });

  it('gives PlaylistsGrid the same numColumns as coverColumns(width)', () => {
    render(
      <PlaylistsGrid
        playlists={[playlist]}
        refresh={idleRefresh()}
        onPlaylistPress={jest.fn()}
        onCreatePress={jest.fn()}
      />,
    );

    expect(screen.UNSAFE_getByType(FlatList).props.numColumns).toBe(coverColumns(width));
  });

  it("sizes PlaylistsGrid's covers from the window width, screen padding and row gap", () => {
    render(
      <PlaylistsGrid
        playlists={[playlist]}
        refresh={idleRefresh()}
        onPlaylistPress={jest.fn()}
        onCreatePress={jest.fn()}
      />,
    );

    const columns = coverColumns(width);
    const expectedSize = cellSize({
      width,
      columns,
      horizontalPadding: spacing.lg,
      gap: spacing.md,
    });

    expect(screen.UNSAFE_getByType(PlaylistCover).props.size).toBe(expectedSize);
  });
});

describe('a measured content width narrower than the window overrides the window fallback', () => {
  const { fireEvent: fire } = require('@testing-library/react-native');
  const { Platform: widePlatform } = require('react-native');
  const originalWideOS = widePlatform.OS;

  beforeEach(() => {
    widePlatform.OS = 'web';
    mockWindowWidth = 1440;
  });

  afterEach(() => {
    widePlatform.OS = originalWideOS;
  });

  it("sizes AlbumsGrid's columns from the grid's own measured width, not the window width", () => {
    render(
      <AlbumsGrid albums={[]} emptyLabel="No albums yet" refresh={idleRefresh()} onAlbumPress={jest.fn()} />,
    );

    const grid = screen.UNSAFE_getByType(FlatList);
    fire(grid, 'layout', { nativeEvent: { layout: { x: 0, y: 0, width: 800, height: 800 } } });

    expect(screen.UNSAFE_getByType(FlatList).props.numColumns).toBe(coverColumns(800));
    expect(screen.UNSAFE_getByType(FlatList).props.numColumns).not.toBe(coverColumns(1440));
  });

  it("sizes PlaylistsGrid's covers from the grid's own measured width, not the window width", () => {
    render(
      <PlaylistsGrid
        playlists={[playlist]}
        refresh={idleRefresh()}
        onPlaylistPress={jest.fn()}
        onCreatePress={jest.fn()}
      />,
    );

    const grid = screen.UNSAFE_getByType(FlatList);
    fire(grid, 'layout', { nativeEvent: { layout: { x: 0, y: 0, width: 800, height: 800 } } });

    const columns = coverColumns(800);
    const expectedSize = cellSize({ width: 800, columns, horizontalPadding: 0, gap: spacing.md });

    expect(screen.UNSAFE_getByType(FlatList).props.numColumns).toBe(columns);
    expect(screen.UNSAFE_getByType(PlaylistCover).props.size).toBe(expectedSize);
  });
});

describe('a native tablet just under the wide breakpoint keeps its column count', () => {
  beforeEach(() => {
    mockWindowWidth = 999;
  });

  it('gives AlbumsGrid 3 cover columns at a 999pt-wide window', () => {
    render(
      <AlbumsGrid albums={[]} emptyLabel="No albums yet" refresh={idleRefresh()} onAlbumPress={jest.fn()} />,
    );

    expect(screen.UNSAFE_getByType(FlatList).props.numColumns).toBe(3);
  });
});

describe('narrow web keeps its pre-#2842 grid once the grid measures itself', () => {
  const { Platform: probePlatform } = require('react-native');
  const { fireEvent: probeFire } = require('@testing-library/react-native');
  const originalOS = probePlatform.OS;

  beforeEach(() => {
    probePlatform.OS = 'web';
    mockWindowWidth = 360;
  });

  afterEach(() => {
    probePlatform.OS = originalOS;
    mockWindowWidth = 390;
  });

  it('gives AlbumsGrid 2 columns at 360px on web before any layout', () => {
    render(
      <AlbumsGrid albums={[]} emptyLabel="No albums yet" refresh={idleRefresh()} onAlbumPress={jest.fn()} />,
    );

    expect(screen.UNSAFE_getByType(FlatList).props.numColumns).toBe(2);
  });

  it('keeps 2 columns and 158px playlist covers at 360px on web once the grid measures 328px', () => {
    render(
      <PlaylistsGrid
        playlists={[playlist]}
        refresh={idleRefresh()}
        onPlaylistPress={jest.fn()}
        onCreatePress={jest.fn()}
      />,
    );

    probeFire(screen.UNSAFE_getByType(FlatList), 'layout', {
      nativeEvent: { layout: { x: 0, y: 0, width: 328, height: 800 } },
    });

    expect(screen.UNSAFE_getByType(FlatList).props.numColumns).toBe(2);
    expect(screen.UNSAFE_getByType(PlaylistCover).props.size).toBe(158);
  });
});
