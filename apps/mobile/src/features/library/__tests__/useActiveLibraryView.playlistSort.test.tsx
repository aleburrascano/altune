// Regression for issue #793: the playlists 'recent' sort must order by the parsed
// `created_at` instant, not by comparing the raw timestamp strings. Go's JSON time
// marshaling trims trailing zero fractional digits, so two same-second timestamps
// can differ only in precision ("...:00Z" vs "...:00.5Z"), where string order and
// instant order disagree.

import { renderHook } from '@testing-library/react-native';

import { asPlaylistId } from '@shared/api-client/ids';
import type { PlaylistResponse } from '@shared/api-client/types';

import { useActiveLibraryView } from '../hooks/useActiveLibraryView';

jest.mock('../hooks/useLibraryHome', () => {
  const empty = { isLoading: false, isRefetching: false, error: null, refetch: jest.fn() };
  return {
    useLibraryTracks: () => ({ ...empty, tracks: [], total: 0, loadAll: jest.fn() }),
    useLibraryAlbums: () => ({ ...empty, albums: [] }),
    useLibraryArtists: () => ({ ...empty, artists: [] }),
    useLibraryIsEmpty: () => false,
  };
});

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

// Every chip's view hook runs on every render, so the composer reads the navigation
// and retry deps whichever chip is selected — they are here to satisfy that, not
// because playlist sorting uses them.
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
