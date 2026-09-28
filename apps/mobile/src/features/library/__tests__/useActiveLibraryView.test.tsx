import { act, renderHook } from '@testing-library/react-native';

import { asPlaylistId, asTrackId } from '@shared/api-client/ids';
import type { PlaylistResponse, TrackResponse } from '@shared/api-client/types';

import { useActiveLibraryView } from '../hooks/useActiveLibraryView';
import {
  ALBUM_SORT_OPTIONS,
  ARTIST_SORT_OPTIONS,
  PLAYLIST_SORT_OPTIONS,
  TRACK_SORT_OPTIONS,
} from '../sort';
import { AlbumsGrid } from '../ui/AlbumsGrid';
import { ArtistsGrid } from '../ui/ArtistsGrid';
import type { LibraryChip } from '../activeView';
import { PlaylistsGrid } from '../ui/PlaylistsGrid';
import { TracksList } from '../ui/TracksList';
import { useLibraryNavigation } from '../hooks/useLibraryNavigation';

let mockUseLibraryTracks: () => unknown;
let mockUseLibraryAlbums: () => unknown;
let mockUseLibraryArtists: () => unknown;

jest.mock('../hooks/useLibraryTracks', () => ({
  useLibraryTracks: () => mockUseLibraryTracks(),
}));

jest.mock('../hooks/useLibraryAlbums', () => ({
  useLibraryAlbums: () => mockUseLibraryAlbums(),
}));

jest.mock('../hooks/useLibraryArtists', () => ({
  useLibraryArtists: () => mockUseLibraryArtists(),
}));

const mockAlbumsError = new Error('albums request failed');
const mockLoadedAlbums = [{ key: 'a1' }, { key: 'a2' }, { key: 'a3' }];
let mockAlbums: { key: string }[] = mockLoadedAlbums;

function stubChipCollections() {
  mockUseLibraryTracks = () => ({
    tracks: [{ id: 't1' }],
    total: 7,
    isLoading: false,
    isRefetching: false,
    error: null,
    isFetchingNextPage: false,
    onEndReached: jest.fn(),
    refetch: jest.fn(),
    loadAll: jest.fn(),
  });
  mockUseLibraryAlbums = () => ({
    albums: mockAlbums,
    isLoading: false,
    isRefetching: false,
    error: mockAlbumsError,
    refetch: jest.fn(),
  });
  mockUseLibraryArtists = () => ({
    artists: [{ key: 'r1' }, { key: 'r2' }],
    isLoading: true,
    isRefetching: false,
    error: null,
    refetch: jest.fn(),
  });
}

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
    stubChipCollections();
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

const mockEmptyState = { isLoading: false, isRefetching: false, error: null, refetch: jest.fn() };

function stubEmptyCollections() {
  mockUseLibraryTracks = () => ({ ...mockEmptyState, tracks: [], total: 0, loadAll: jest.fn() });
  mockUseLibraryAlbums = () => ({ ...mockEmptyState, albums: [] });
  mockUseLibraryArtists = () => ({ ...mockEmptyState, artists: [] });
}

function playlist(id: string, name: string, createdAt: string): PlaylistResponse {
  return {
    id: asPlaylistId(id),
    name,
    track_count: 0,
    preview_artwork_urls: [],
    created_at: createdAt,
    updated_at: createdAt,
  };
}

function sortedIds(playlists: PlaylistResponse[], sort: 'recent' | 'az'): string[] {
  const deps = {
    pl: { playlists, isRefetchingPlaylists: false, refetchPlaylists: jest.fn() },
    navigation: {
      navigateToTrack: jest.fn(),
      navigateToAlbum: jest.fn(),
      navigateToArtist: jest.fn(),
    },
    retryMutation: { mutate: jest.fn(), isPending: false, variables: undefined },
  } as unknown as Parameters<typeof useActiveLibraryView>[3];
  const sorts = { playlists: sort, tracks: 'recent', albums: 'az', artists: 'az' } as const;
  const { result } = renderHook(() => useActiveLibraryView('playlists', sorts, '', deps));
  return result.current.playlists.map((p) => p.id);
}

describe('useActiveLibraryView — playlist sort', () => {
  beforeEach(() => {
    stubEmptyCollections();
  });

  it('orders same-second timestamps of differing precision by instant, newest first', () => {
    const whole = playlist('whole', 'Whole', '2026-01-01T00:00:00Z');
    const half = playlist('half', 'Half', '2026-01-01T00:00:00.5Z');
    const tenth = playlist('tenth', 'Tenth', '2026-01-01T00:00:00.123Z');

    expect(sortedIds([whole, tenth, half], 'recent')).toEqual(['half', 'tenth', 'whole']);
  });

  it('orders timestamps with different UTC offsets by instant', () => {
    const earlier = playlist('earlier', 'Earlier', '2026-01-01T10:00:00+02:00');
    const later = playlist('later', 'Later', '2026-01-01T09:00:00Z');

    expect(sortedIds([earlier, later], 'recent')).toEqual(['later', 'earlier']);
  });

  it('sorts unparseable timestamps last', () => {
    const bad = playlist('bad', 'Bad', 'not-a-date');
    const good = playlist('good', 'Good', '2020-01-01T00:00:00Z');

    expect(sortedIds([bad, good], 'recent')).toEqual(['good', 'bad']);
  });

  it('still sorts by name for az', () => {
    const b = playlist('b', 'Beta', '2026-01-01T00:00:00Z');
    const a = playlist('a', 'Alpha', '2020-01-01T00:00:00Z');

    expect(sortedIds([b, a], 'az')).toEqual(['a', 'b']);
  });
});

function trackResponse(id: string): TrackResponse {
  return {
    id: asTrackId(id),
    title: `Track ${id}`,
    artist: 'An Artist',
    album: null,
    duration_seconds: 180,
    added_at: '2024-01-01T00:00:00Z',
    acquisition_status: 'ready',
    artwork_url: null,
    failure_reason: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
  };
}

const mockFullLibrary = Array.from({ length: 250 }, (_, i) => trackResponse(`t${i}`));
const mockLoadedPages = mockFullLibrary.slice(0, 200);
const mockLoadAll = jest.fn(() => Promise.resolve(mockFullLibrary));

function stubPagedLibrary() {
  mockUseLibraryTracks = () => ({
    tracks: mockLoadedPages,
    total: mockFullLibrary.length,
    isLoading: false,
    isRefetching: false,
    error: null,
    isFetchingNextPage: false,
    onEndReached: jest.fn(),
    refetch: jest.fn(),
    loadAll: mockLoadAll,
  });
  mockUseLibraryAlbums = () => ({
    albums: [],
    isLoading: false,
    isRefetching: false,
    error: null,
    refetch: jest.fn(),
  });
  mockUseLibraryArtists = () => ({
    artists: [],
    isLoading: false,
    isRefetching: false,
    error: null,
    refetch: jest.fn(),
  });
}

function makeShuffleDeps() {
  const shuffleFromList = jest.fn();
  const playFromList = jest.fn();
  const deps = {
    pl: {
      playlists: [],
      isRefetchingPlaylists: false,
      refetchPlaylists: jest.fn(),
      setCreateModalVisible: jest.fn(),
    },
    router: { push: jest.fn(), replace: jest.fn() },
    navigation: {
      navigateToTrack: jest.fn(),
      navigateToAlbum: jest.fn(),
      navigateToArtist: jest.fn(),
    },
    selection: {},
    queue: { shuffleFromList, playFromList },
    playback: {},
    retryMutation: { mutate: jest.fn(), isPending: false, variables: undefined },
    onTrackMore: jest.fn(),
  } as unknown as Parameters<typeof useActiveLibraryView>[3];
  return { deps, shuffleFromList };
}

describe('useActiveLibraryView — shuffleWholeLibrary', () => {
  beforeEach(() => {
    stubPagedLibrary();
    mockLoadAll.mockClear();
  });

  it('seeds the shuffle from the full library (loadAll), not the loaded pages', async () => {
    const { deps, shuffleFromList } = makeShuffleDeps();
    const { result } = renderHook(() => useActiveLibraryView('tracks', SORTS, '', deps));

    await act(async () => {
      await result.current.shuffleWholeLibrary();
    });

    expect(mockLoadAll).toHaveBeenCalledTimes(1);
    expect(shuffleFromList).toHaveBeenCalledTimes(1);
    const [playable, source] = shuffleFromList.mock.calls[0]!;
    expect(playable).toHaveLength(mockFullLibrary.length);
    expect(playable.length).toBeGreaterThan(mockLoadedPages.length);
    expect(source).toEqual({ kind: 'library' });
    const seededIds = (playable as { source: { trackId: string } }[]).map((t) => t.source.trackId);
    expect(seededIds).toContain(mockFullLibrary[249]!.id);
  });
});

describe('useActiveLibraryView — playlist press', () => {
  beforeEach(() => {
    stubEmptyCollections();
  });

  it('pushes /library/playlist/<id> when a playlist is pressed', () => {
    const fakeRouter = { push: jest.fn() } as unknown as Parameters<typeof useLibraryNavigation>[0];
    const { result: navigationResult } = renderHook(() => useLibraryNavigation(fakeRouter));
    const deps = {
      ...makeDeps(),
      router: fakeRouter,
      navigation: navigationResult.current,
    } as unknown as Parameters<typeof useActiveLibraryView>[3];

    const { result } = renderHook(() => useActiveLibraryView('playlists', SORTS, '', deps));
    const props = result.current.active.content.props as {
      onPlaylistPress: (playlist: PlaylistResponse) => void;
    };
    const pressedPlaylist = (deps as unknown as { pl: { playlists: PlaylistResponse[] } }).pl
      .playlists[0]!;
    props.onPlaylistPress(pressedPlaylist);

    expect(fakeRouter.push).toHaveBeenCalledWith(`/library/playlist/${pressedPlaylist.id}`);
  });
});

describe('useActiveLibraryView — playlist press without a router in deps', () => {
  beforeEach(() => {
    stubEmptyCollections();
  });

  it('opens the pressed playlist through navigation when deps carry no router', () => {
    const fakeRouter = { push: jest.fn() } as unknown as Parameters<typeof useLibraryNavigation>[0];
    const { result: navigationResult } = renderHook(() => useLibraryNavigation(fakeRouter));
    const second: PlaylistResponse = {
      id: asPlaylistId('p2'),
      name: 'Second Playlist',
      track_count: 3,
      preview_artwork_urls: [],
      created_at: '2025-06-01T00:00:00Z',
      updated_at: '2025-06-01T00:00:00Z',
    };
    const withoutRouter = { ...makeDeps() } as unknown as Record<string, unknown>;
    delete withoutRouter.router;
    const deps = {
      ...withoutRouter,
      navigation: navigationResult.current,
    } as unknown as Parameters<typeof useActiveLibraryView>[3];

    const { result } = renderHook(() => useActiveLibraryView('playlists', SORTS, '', deps));
    const props = result.current.active.content.props as {
      onPlaylistPress: (playlist: PlaylistResponse) => void;
    };
    props.onPlaylistPress(second);

    expect(fakeRouter.push).toHaveBeenCalledTimes(1);
    expect(fakeRouter.push).toHaveBeenCalledWith('/library/playlist/p2');
  });
});
