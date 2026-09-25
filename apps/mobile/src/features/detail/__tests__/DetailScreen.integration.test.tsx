// #1656: the detail screen read "is this library-only?" from the handoff result,
// before useResolveMissingSources had backfilled its sources. A successful
// backfill was then thrown away: the screen kept rendering library-only data and
// the artist path fell back to a fuzzy search it no longer needed.

import { render, screen } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { clearDetailHandoffs, detailHref } from '@shared/lib/detail-handoff';

import { DetailScreen } from '../ui/DetailScreen';

let mockParams: { handoff?: string } = {};
jest.mock('expo-router', () => {
  const { Text: RNText } = jest.requireActual('react-native');
  return {
    useLocalSearchParams: () => mockParams,
    useRouter: () => ({
      push: jest.fn(),
      replace: jest.fn(),
      back: jest.fn(),
      canGoBack: () => true,
    }),
    useSegments: () => ['(tabs)', 'library', 'detail'],
    Redirect: ({ href }: { href: string }) => <RNText>{`redirect:${href}`}</RNText>,
  };
});

// The resolver under the real screen: a library result arrives with no sources
// and leaves with the exact match's sources backfilled.
jest.mock('../hooks/useResolveMissingSources', () => ({
  useResolveMissingSources: (result: DiscoveryResult) => ({
    resolved:
      result.sources.length === 0
        ? { ...result, sources: [{ provider: 'deezer', external_id: 'd1', url: 'https://d/1' }] }
        : result,
    isResolving: false,
  }),
}));

jest.mock('../hooks/useDetailEnrichments', () => ({
  useDetailEnrichments: () => ({
    errors: { musicbrainz: false, deezer: false, lastfm: false },
  }),
}));
jest.mock('../hooks/useLateralNav', () => ({ useLateralNav: () => ({}) }));
jest.mock('../ui/secondaryLine', () => ({ secondaryLine: () => null }));
jest.mock('../hooks/useSaveTrack', () => ({
  useSaveTrack: () => ({ mutate: jest.fn(), mutateAsync: jest.fn(), isPending: false }),
}));
jest.mock('../hooks/useOwnedPlayback', () => ({
  useOwnedPlayback: () => ({
    owned: { playable: [], unownedCount: 0, acquiringCount: 0 },
    playButton: { label: 'Play', disabled: true },
    onPlayOwned: jest.fn(),
    ownedFor: () => null,
    onQuickSave: jest.fn(),
  }),
}));

// The fuzzy fallbacks a library-only entity would use. They stay empty here, so
// anything the screen renders came from the resolved sources or the library.
jest.mock('../hooks/useArtistDiscovery', () => ({
  useArtistDiscovery: () => ({
    imageUrl: null,
    sources: [],
    isLoading: false,
    isError: false,
    refetch: jest.fn(),
  }),
}));
jest.mock('../hooks/useAlbumDiscovery', () => ({
  useAlbumDiscovery: () => ({
    tracks: [],
    isLoading: false,
    isError: false,
    failure: null,
    refetch: jest.fn(),
  }),
}));
jest.mock('../hooks/useLibraryAlbumsForArtist', () => ({ useLibraryAlbumsForArtist: () => [] }));

jest.mock('../hooks/useAlbumTracks', () => ({
  useAlbumTracks: () => ({
    tracks: [
      {
        kind: 'track',
        title: 'Dreams',
        subtitle: 'Fleetwood Mac',
        image_url: null,
        confidence: 'high',
        sources: [{ provider: 'deezer', external_id: 'd-dreams', url: 'https://d/dreams' }],
        extras: {},
      },
    ],
    isLoading: false,
    isError: false,
    failure: null,
    refetch: jest.fn(),
  }),
}));

jest.mock('../hooks/useArtistContent', () => ({
  useArtistContent: () => ({
    topTracks: [
      {
        kind: 'track',
        title: 'Dayvan Cowboy',
        subtitle: 'Boards of Canada',
        image_url: null,
        confidence: 'high',
        sources: [{ provider: 'deezer', external_id: 'd-dayvan', url: 'https://d/dayvan' }],
        extras: {},
      },
    ],
    albums: [],
    isLoadingTracks: false,
    isLoadingAlbums: false,
    isErrorTracks: false,
    isErrorAlbums: false,
    tracksFailure: null,
    albumsFailure: null,
    refetchTracks: jest.fn(),
    refetchAlbums: jest.fn(),
  }),
}));

jest.mock('../hooks/useLibraryTracks', () => {
  const { asTrackId } = require('@shared/api-client/ids');
  const libraryTrack = (id: string, title: string, artist: string, album: string) => ({
    id: asTrackId(id),
    title,
    artist,
    album,
    duration_seconds: 271,
    added_at: '2024-01-01T00:00:00Z',
    acquisition_status: 'ready',
    artwork_url: null,
    failure_reason: null,
    year: 1977,
    genre: null,
    track_number: 1,
    album_artist: artist,
    isrc: null,
    audio_ref: null,
  });
  return {
    useLibraryTracksForAlbum: () => [
      libraryTrack('trk-chain', 'The Chain', 'Fleetwood Mac', 'Rumours'),
    ],
    useLibraryTracksForArtist: () => [
      libraryTrack('trk-roygbiv', 'Roygbiv', 'Boards of Canada', 'Music Has the Right to Children'),
    ],
  };
});

function libraryResult(kind: 'album' | 'artist'): DiscoveryResult {
  return kind === 'album'
    ? {
        kind: 'album',
        title: 'Rumours',
        subtitle: 'Fleetwood Mac',
        image_url: null,
        confidence: 'high',
        sources: [],
        extras: {},
      }
    : {
        kind: 'artist',
        title: 'Boards of Canada',
        subtitle: null,
        image_url: null,
        confidence: 'high',
        sources: [],
        extras: {},
      };
}

function renderDetail(result: DiscoveryResult): void {
  mockParams = { handoff: detailHref('/library/detail', result, 'search-1').params.handoff };
  render(<DetailScreen />);
}

beforeEach(() => {
  clearDetailHandoffs();
  mockParams = {};
});

describe('DetailScreen after the resolver backfills a library result its sources', () => {
  it('lists the resolved album tracks instead of the library-only ones', () => {
    renderDetail(libraryResult('album'));

    expect(screen.getByText('Dreams')).toBeTruthy();
    expect(screen.queryByText('The Chain')).toBeNull();
  });

  it('shows the resolved artist top tracks instead of the library-only ones', () => {
    renderDetail(libraryResult('artist'));

    expect(screen.getByText('POPULAR TRACKS')).toBeTruthy();
    expect(screen.getByText('Dayvan Cowboy')).toBeTruthy();
    expect(screen.queryByText('Roygbiv')).toBeNull();
  });
});
