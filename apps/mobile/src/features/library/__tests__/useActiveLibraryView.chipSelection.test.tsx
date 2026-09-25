// The four chip views are built by four separate hooks and picked by chip (#1691).
// A mis-wired pick is invisible to the type-checker — every view is an ActiveView —
// so each chip is pinned to its own grid, noun, sort options, count, and to its own
// loading/error state rather than a neighbour's.

import { renderHook } from '@testing-library/react-native';

import { asPlaylistId } from '@shared/api-client/ids';

import { useActiveLibraryView } from '../hooks/useActiveLibraryView';
import {
  ALBUM_SORT_OPTIONS,
  ARTIST_SORT_OPTIONS,
  PLAYLIST_SORT_OPTIONS,
  TRACK_SORT_OPTIONS,
} from '../sort';
import { AlbumsGrid } from '../ui/AlbumsGrid';
import { ArtistsGrid } from '../ui/ArtistsGrid';
import type { LibraryChip } from '../ui/LibraryChips';
import { PlaylistsGrid } from '../ui/PlaylistsGrid';
import { TracksList } from '../ui/TracksList';

const mockAlbumsError = new Error('albums request failed');
const mockLoadedAlbums = [{ key: 'a1' }, { key: 'a2' }, { key: 'a3' }];
let mockAlbums: { key: string }[] = mockLoadedAlbums;

// Each collection gets a distinct size, so a chip reading a neighbour's state shows
// up as the wrong count rather than coincidentally matching.
jest.mock('../hooks/useLibraryTracks', () => ({
  useLibraryTracks: () => ({
    tracks: [{ id: 't1' }],
    total: 7,
    isLoading: false,
    isRefetching: false,
    error: null,
    isFetchingNextPage: false,
    onEndReached: jest.fn(),
    refetch: jest.fn(),
    loadAll: jest.fn(),
  }),
}));

jest.mock('../hooks/useLibraryAlbums', () => ({
  useLibraryAlbums: () => ({
    albums: mockAlbums,
    isLoading: false,
    isRefetching: false,
    error: mockAlbumsError,
    refetch: jest.fn(),
  }),
}));

jest.mock('../hooks/useLibraryArtists', () => ({
  useLibraryArtists: () => ({
    artists: [{ key: 'r1' }, { key: 'r2' }],
    isLoading: true,
    isRefetching: false,
    error: null,
    refetch: jest.fn(),
  }),
}));

const SORTS = { playlists: 'recent', tracks: 'recent', albums: 'az', artists: 'az' } as const;

function makeDeps(): Parameters<typeof useActiveLibraryView>[3] {
  return {
    pl: {
      playlists: [
        {
          id: asPlaylistId('p1'),
          name: 'Only Playlist',
          track_count: 0,
          preview_artwork_urls: [],
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
      ],
      isRefetchingPlaylists: false,
      refetchPlaylists: jest.fn(),
      setCreateModalVisible: jest.fn(),
    },
    router: { push: jest.fn() },
    navigation: {
      navigateToTrack: jest.fn(),
      navigateToAlbum: jest.fn(),
      navigateToArtist: jest.fn(),
    },
    selection: {},
    queue: { playFromList: jest.fn(), shuffleFromList: jest.fn() },
    playback: {},
    retryMutation: { mutate: jest.fn(), isPending: false, variables: undefined },
    onTrackMore: jest.fn(),
  } as unknown as Parameters<typeof useActiveLibraryView>[3];
}

function activeViewFor(chip: LibraryChip) {
  const { result } = renderHook(() => useActiveLibraryView(chip, SORTS, '', makeDeps()));
  return result.current.active;
}

describe('useActiveLibraryView — the chip picks its own view', () => {
  beforeEach(() => {
    mockAlbums = mockLoadedAlbums;
  });

  it('renders the playlists grid with the playlist noun, options and count', () => {
    const active = activeViewFor('playlists');

    expect(active.content.type).toBe(PlaylistsGrid);
    expect(active.noun).toBe('playlist');
    expect(active.options).toBe(PLAYLIST_SORT_OPTIONS);
    expect(active.count).toBe(1);
  });

  it('renders the tracks list with the track noun, options and total', () => {
    const active = activeViewFor('tracks');

    expect(active.content.type).toBe(TracksList);
    expect(active.noun).toBe('track');
    expect(active.options).toBe(TRACK_SORT_OPTIONS);
    expect(active.count).toBe(7);
  });

  it('renders the albums grid with the album noun, options and count', () => {
    const active = activeViewFor('albums');

    expect(active.content.type).toBe(AlbumsGrid);
    expect(active.noun).toBe('album');
    expect(active.options).toBe(ALBUM_SORT_OPTIONS);
    expect(active.count).toBe(3);
  });

  it('renders the artists grid with the artist noun, options and count', () => {
    const active = activeViewFor('artists');

    expect(active.content.type).toBe(ArtistsGrid);
    expect(active.noun).toBe('artist');
    expect(active.options).toBe(ARTIST_SORT_OPTIONS);
    expect(active.count).toBe(2);
  });

  it("surfaces the selected chip's own failure, never a neighbour's", () => {
    mockAlbums = [];
    expect(activeViewFor('albums').error).toBe(mockAlbumsError);
    expect(activeViewFor('tracks').error).toBeNull();
    expect(activeViewFor('artists').error).toBeNull();
  });

  it("surfaces the selected chip's own loading state", () => {
    expect(activeViewFor('artists').isLoading).toBe(true);
    expect(activeViewFor('albums').isLoading).toBe(false);
    expect(activeViewFor('playlists').isLoading).toBe(false);
  });
});
