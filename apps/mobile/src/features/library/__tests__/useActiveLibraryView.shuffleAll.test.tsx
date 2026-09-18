// Regression for issue #30: shuffling the library must draw from the WHOLE
// library (via loadAll's full pagination), not just the pages already rendered on
// screen. The on-screen list is capped at TRACKS_PAGE_SIZE-driven pages; a naive
// shuffle over that would loop the same first ~200 tracks in the car.

import { act, renderHook } from '@testing-library/react-native';

import { asTrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';

import { useActiveLibraryView } from '../hooks/useActiveLibraryView';

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

// Full library is 250 tracks; only the first 200 are "loaded" on screen.
const mockFullLibrary = Array.from({ length: 250 }, (_, i) => trackResponse(`t${i}`));
const mockLoadedPages = mockFullLibrary.slice(0, 200);
const mockLoadAll = jest.fn(() => Promise.resolve(mockFullLibrary));

jest.mock('../hooks/useLibraryTracks', () => ({
  useLibraryTracks: () => ({
    tracks: mockLoadedPages,
    total: mockFullLibrary.length,
    isLoading: false,
    isRefetching: false,
    error: null,
    isFetchingNextPage: false,
    onEndReached: jest.fn(),
    refetch: jest.fn(),
    loadAll: mockLoadAll,
  }),
}));

jest.mock('../hooks/useLibraryAlbums', () => ({
  useLibraryAlbums: () => ({
    albums: [],
    isLoading: false,
    isRefetching: false,
    error: null,
    refetch: jest.fn(),
  }),
}));

jest.mock('../hooks/useLibraryArtists', () => ({
  useLibraryArtists: () => ({
    artists: [],
    isLoading: false,
    isRefetching: false,
    error: null,
    refetch: jest.fn(),
  }),
}));

function makeDeps() {
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
    navigation: { navigateToTrack: jest.fn(), navigateToAlbum: jest.fn(), navigateToArtist: jest.fn() },
    selection: {},
    queue: { shuffleFromList, playFromList },
    playback: {},
    retryMutation: { mutate: jest.fn(), isPending: false, variables: undefined },
    onTrackMore: jest.fn(),
  } as unknown as Parameters<typeof useActiveLibraryView>[3];
  return { deps, shuffleFromList };
}

const SORTS = { playlists: 'recent', tracks: 'recent', albums: 'az', artists: 'az' } as const;

beforeEach(() => {
  mockLoadAll.mockClear();
});

describe('useActiveLibraryView — shuffleWholeLibrary', () => {
  it('seeds the shuffle from the full library (loadAll), not the loaded pages', async () => {
    const { deps, shuffleFromList } = makeDeps();
    const { result } = renderHook(() => useActiveLibraryView('tracks', SORTS, '', deps));

    await act(async () => {
      await result.current.shuffleWholeLibrary();
    });

    expect(mockLoadAll).toHaveBeenCalledTimes(1);
    expect(shuffleFromList).toHaveBeenCalledTimes(1);
    const [playable, source] = shuffleFromList.mock.calls[0]!;
    // The queue is seeded with all 250 tracks — the whole library — not the 200
    // pages rendered on screen.
    expect(playable).toHaveLength(mockFullLibrary.length);
    expect(playable.length).toBeGreaterThan(mockLoadedPages.length);
    expect(source).toEqual({ kind: 'library' });
    // The last track, absent from the loaded pages, is present in the queue.
    const seededIds = (playable as { source: { trackId: string } }[]).map((t) => t.source.trackId);
    expect(seededIds).toContain(mockFullLibrary[249]!.id);
  });
});
