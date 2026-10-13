import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import type { ReactElement, ReactNode } from 'react';
import { asPlaylistId, asTrackId } from '@shared/apiClient/ids';
import type { AlbumGroup, ArtistGroup } from '@shared/apiClient/library';
import type { PlaylistResponse } from '@shared/apiClient/playlistResponse';
import type { TrackResponse } from '@shared/apiClient/trackResponse';
import { useActiveLibraryView } from '@features/library/hooks/useActiveLibraryView';
import { useSelection } from '@features/library/hooks/useSelection';
import {
  ALBUM_SORT_OPTIONS,
  ARTIST_SORT_OPTIONS,
  PLAYLIST_SORT_OPTIONS,
  TRACK_SORT_OPTIONS,
  type LibraryChip,
  type SortKey,
} from '@features/library/domain/libraryView';
import {
  createMemoryLibraryPort,
  type MemoryLibrarySeed,
} from '@features/library/__tests__/memoryLibraryPort';
import { track } from '@features/library/__tests__/trackMutationFixtures';
import { LibraryPortProvider } from '@features/library/ports/LibraryPortProvider';
import type { LibraryPort } from '@features/library/ports/libraryPort';
import { useLibraryNavigation } from '@features/library/hooks/useLibraryNavigation';
import type { Navigator } from '@shared/navigation/navigator';

type ActiveLibraryView = ReturnType<typeof useActiveLibraryView>;
type Deps = Parameters<typeof useActiveLibraryView>[3];
type Sorts = Record<LibraryChip, SortKey>;

const SEEDED_TRACKS = 7;
const SEEDED_ALBUMS = 3;
const SEEDED_ARTISTS = 2;
const BEYOND_FIRST_PAGE = 50;
const TRACKS_PAGE_SIZE = 200;

const SORTS: Sorts = { playlists: 'recent', tracks: 'recent', albums: 'az', artists: 'az' };

const base = track('base');
function row(id: string): TrackResponse {
  return { ...base, id: asTrackId(id), title: `Track ${id}` };
}

function album(key: string): AlbumGroup {
  return {
    key,
    album: `Album ${key}`,
    artist: 'An Artist',
    artwork_url: null,
    year: null,
    track_count: 1,
    most_recent_added_at: '2026-01-01T00:00:00Z',
  };
}

function artist(key: string): ArtistGroup {
  return {
    key,
    artist: `Artist ${key}`,
    artwork_url: null,
    track_count: 1,
    most_recent_added_at: '2026-01-01T00:00:00Z',
  };
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

const onlyPlaylist = playlist('p1', 'Only Playlist', '2026-01-01T00:00:00Z');

type FakeDeps = Omit<Deps, 'selection'>;

function asDeps(fake: unknown): FakeDeps {
  return fake as FakeDeps;
}

function makeDeps(over: Record<string, unknown> = {}): FakeDeps {
  return asDeps({
    pl: {
      playlists: [onlyPlaylist],
      isRefetchingPlaylists: false,
      refetchPlaylists: jest.fn(),
      loadMorePlaylists: jest.fn(),
      setCreateModalVisible: jest.fn(),
    },
    navigation: {
      navigateToTrack: jest.fn(),
      navigateToAlbum: jest.fn(),
      navigateToArtist: jest.fn(),
      navigateToPlaylist: jest.fn(),
    },
    queue: { playFromList: jest.fn(), shuffleFromList: jest.fn() },
    playback: {},
    retryMutation: { mutate: jest.fn(), isInFlight: () => false },
    onTrackMore: jest.fn(),
    ...over,
  });
}

let client: QueryClient;

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
});

afterEach(() => client.clear());

function renderActive(
  chip: LibraryChip,
  port: LibraryPort,
  deps: FakeDeps = makeDeps(),
  sorts: Sorts = SORTS,
) {
  const seen: { current: ActiveLibraryView | null } = { current: null };
  function Library(): ReactElement {
    const selection = useSelection();
    const view = useActiveLibraryView(chip, sorts, '', { ...deps, selection } as Deps);
    seen.current = view;
    return <>{view.active.content}</>;
  }
  render(
    <QueryClientProvider client={client}>
      <LibraryPortProvider port={port}>
        <Library />
      </LibraryPortProvider>
    </QueryClientProvider>,
  );
  return () => seen.current!;
}

function seededPort(seed: MemoryLibrarySeed = {}) {
  return createMemoryLibraryPort({
    tracks: ['t1', 't2', 't3', 't4', 't5', 't6', 't7'].map(row),
    albums: [album('a1'), album('a2'), album('a3')],
    artists: [artist('r1'), artist('r2')],
    ...seed,
  });
}

describe('useActiveLibraryView — the chip picks its own view', () => {
  it('renders the playlists grid with the playlist noun, options and count', () => {
    const view = renderActive('playlists', seededPort());

    expect(screen.getByTestId('library-playlists-grid')).toBeTruthy();
    expect(screen.getByText('Only Playlist')).toBeTruthy();
    const { active } = view();
    expect(active.noun).toBe('playlist');
    expect(active.options).toBe(PLAYLIST_SORT_OPTIONS);
    expect(active.count).toBe(1);
  });

  it('renders the tracks list with the track noun, options and total', async () => {
    const memory = seededPort();
    const port = {
      ...memory,
      getTracks: jest.fn<
        ReturnType<LibraryPort['getTracks']>,
        Parameters<LibraryPort['getTracks']>
      >((params, signal) => memory.getTracks({ ...params, limit: 1 }, signal)),
    };
    const view = renderActive('tracks', port);

    await waitFor(() => expect(screen.getByText('Track t1')).toBeTruthy());
    expect(screen.getByTestId('library-tracks-list')).toBeTruthy();
    expect(screen.queryByText('Track t2')).toBeNull();
    const { active } = view();
    expect(active.noun).toBe('track');
    expect(active.options).toBe(TRACK_SORT_OPTIONS);
    expect(active.count).toBe(SEEDED_TRACKS);
  });

  it('renders the albums grid with the album noun, options and count', async () => {
    const view = renderActive('albums', seededPort());

    await waitFor(() => expect(view().active.count).toBe(SEEDED_ALBUMS));
    expect(screen.getByTestId('library-albums-grid')).toBeTruthy();
    expect(screen.getByTestId('library-album-a3')).toBeTruthy();
    const { active } = view();
    expect(active.noun).toBe('album');
    expect(active.options).toBe(ALBUM_SORT_OPTIONS);
  });

  it('renders the artists grid with the artist noun, options and count', async () => {
    const view = renderActive('artists', seededPort());

    await waitFor(() => expect(view().active.count).toBe(SEEDED_ARTISTS));
    expect(screen.getByTestId('library-artists-grid')).toBeTruthy();
    expect(screen.getByTestId('library-artist-r2')).toBeTruthy();
    const { active } = view();
    expect(active.noun).toBe('artist');
    expect(active.options).toBe(ARTIST_SORT_OPTIONS);
  });

  describe('with an albums request that fails', () => {
    const albumsError = new Error('albums request failed');

    function failingAlbumsPort(): LibraryPort {
      return { ...seededPort(), getLibraryAlbums: () => Promise.reject(albumsError) };
    }

    it("surfaces the selected chip's own failure, never a neighbour's", async () => {
      const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
      const albums = renderActive('albums', failingAlbumsPort());
      await waitFor(() => expect(albums().active.error).toBe(albumsError));

      const tracks = renderActive('tracks', failingAlbumsPort());
      await waitFor(() => expect(tracks().active.count).toBe(SEEDED_TRACKS));
      expect(tracks().active.error).toBeNull();

      const artists = renderActive('artists', failingAlbumsPort());
      await waitFor(() => expect(artists().active.count).toBe(SEEDED_ARTISTS));
      expect(artists().active.error).toBeNull();
      warn.mockRestore();
    });
  });

  it("surfaces the selected chip's own loading state", async () => {
    const holdingArtists: LibraryPort = {
      ...seededPort(),
      getLibraryArtists: () => new Promise(() => undefined),
    };

    const artists = renderActive('artists', holdingArtists);
    expect(artists().active.isLoading).toBe(true);

    const albums = renderActive('albums', holdingArtists);
    await waitFor(() => expect(albums().active.count).toBe(SEEDED_ALBUMS));
    expect(albums().active.isLoading).toBe(false);

    const playlists = renderActive('playlists', holdingArtists);
    expect(playlists().active.isLoading).toBe(false);
    expect(artists().active.isLoading).toBe(true);
  });
});

describe('useActiveLibraryView — playlist sort', () => {
  function sortedIds(playlists: PlaylistResponse[], sort: 'recent' | 'az'): string[] {
    const deps = makeDeps({
      pl: { ...makeDeps().pl, playlists },
    });
    const view = renderActive('playlists', createMemoryLibraryPort(), deps, {
      ...SORTS,
      playlists: sort,
    });
    return view().playlists.map((p) => p.id);
  }

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

describe('useActiveLibraryView — shuffleWholeLibrary', () => {
  const fullLibrary = Array.from({ length: TRACKS_PAGE_SIZE + BEYOND_FIRST_PAGE }, (_, i) =>
    row(`t${i}`),
  );

  it('seeds the shuffle from the full library, not the loaded pages', async () => {
    const shuffleFromList = jest.fn();
    const deps = makeDeps({ queue: { shuffleFromList, playFromList: jest.fn() } });
    const view = renderActive('tracks', createMemoryLibraryPort({ tracks: fullLibrary }), deps);
    await waitFor(() => expect(view().tracks).toHaveLength(TRACKS_PAGE_SIZE));

    await act(async () => {
      await view().shuffleWholeLibrary();
    });

    expect(shuffleFromList).toHaveBeenCalledTimes(1);
    const [playable, source] = shuffleFromList.mock.calls[0]!;
    expect(playable).toHaveLength(fullLibrary.length);
    expect(source).toEqual({ kind: 'library' });
    const seededIds = (playable as { source: { trackId: string } }[]).map((t) => t.source.trackId);
    expect(seededIds).toContain(fullLibrary[fullLibrary.length - 1]!.id);
  });
});

describe('useActiveLibraryView — playlist press', () => {
  const second = playlist('p2', 'Second Playlist', '2025-06-01T00:00:00Z');

  type NavigatedLibraryProps = { push: jest.Mock };

  function NavigatedLibrary({ push }: NavigatedLibraryProps): ReactElement {
    const router: Navigator = { push, replace: jest.fn(), back: jest.fn(), canGoBack: () => false };
    const navigation = useLibraryNavigation(router);
    const selection = useSelection();
    const deps = makeDeps({ pl: { ...makeDeps().pl, playlists: [onlyPlaylist, second] } });
    const { active } = useActiveLibraryView('playlists', SORTS, '', {
      ...deps,
      navigation,
      selection,
    } as Deps);
    return <>{active.content}</>;
  }

  function renderNavigated(push: jest.Mock): void {
    function wrapper({ children }: { children: ReactNode }) {
      return (
        <QueryClientProvider client={client}>
          <LibraryPortProvider port={createMemoryLibraryPort()}>{children}</LibraryPortProvider>
        </QueryClientProvider>
      );
    }
    render(<NavigatedLibrary push={push} />, { wrapper });
  }

  it('pushes /library/playlist/<id> when a playlist is pressed', () => {
    const push = jest.fn();
    renderNavigated(push);

    fireEvent.press(screen.getByTestId('library-playlist-p1'));

    expect(push).toHaveBeenCalledTimes(1);
    expect(push).toHaveBeenCalledWith('/library/playlist/p1');
  });

  it('opens the playlist that was pressed, not the first one', () => {
    const push = jest.fn();
    renderNavigated(push);

    fireEvent.press(screen.getByTestId('library-playlist-p2'));

    expect(push).toHaveBeenCalledTimes(1);
    expect(push).toHaveBeenCalledWith('/library/playlist/p2');
  });
});
