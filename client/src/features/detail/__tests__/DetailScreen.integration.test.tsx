import { fireEvent, render, screen } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/apiClient/discovery';
import { detailHref } from '@shared/navigation/detailHandoff';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';

import { DetailScreen } from '@features/detail/ui/DetailScreen';
import {
  createTestQueryClient,
  createPlaybackWrapper,
  signInTestUser,
} from '@testing/queryHarness';

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

const { __http } = require('@testing/doubles/fetch.js');

const SEARCH = 'GET /v1/discovery/search';
const ALBUM_TRACKS = 'GET /v1/discovery/albums/deezer/d1/tracks';
const ARTIST_CONTENT = 'GET /v1/discovery/artists/deezer/a1/content';
const LIBRARY_TRACKS = 'GET /v1/tracks';

function resolved(kind: 'album' | 'artist', title: string, subtitle: string | null, id: string) {
  return {
    kind,
    title,
    subtitle,
    image_url: null,
    confidence: 'high',
    sources: [{ provider: 'deezer', external_id: id, url: `https://d/${id}` }],
    extras: {},
  };
}

function discoveredTrack(title: string, subtitle: string, id: string) {
  return {
    kind: 'track',
    title,
    subtitle,
    image_url: null,
    confidence: 'high',
    sources: [{ provider: 'deezer', external_id: id, url: `https://d/${id}` }],
    extras: {},
  };
}

function libraryTrack(id: string, title: string, artist: string, album: string) {
  return {
    id,
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
  };
}

function replyWithCatalogue(): void {
  __http.replyAll({ status: 200, json: { items: [], total: 0 } });
  __http.reply(SEARCH, {
    status: 200,
    json: {
      query: 'q',
      query_norm: 'q',
      results: [
        resolved('album', 'Rumours', 'Fleetwood Mac', 'd1'),
        resolved('artist', 'Boards of Canada', null, 'a1'),
      ],
      sections: [],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: 2,
      offset: 0,
      has_more: false,
    },
  });
  __http.reply(ALBUM_TRACKS, {
    status: 200,
    json: {
      items: [discoveredTrack('Dreams', 'Fleetwood Mac', 'd-dreams')],
      provider_name: 'deezer',
      status: 'ok',
    },
  });
  __http.reply(ARTIST_CONTENT, {
    status: 200,
    json: {
      top_tracks: {
        items: [discoveredTrack('Dayvan Cowboy', 'Boards of Canada', 'd-dayvan')],
        provider_name: 'deezer',
        status: 'ok',
      },
      albums: { items: [], provider_name: 'deezer', status: 'ok' },
    },
  });
  __http.reply(LIBRARY_TRACKS, {
    status: 200,
    json: {
      items: [
        libraryTrack('trk-chain', 'The Chain', 'Fleetwood Mac', 'Rumours'),
        libraryTrack(
          'trk-roygbiv',
          'Roygbiv',
          'Boards of Canada',
          'Music Has the Right to Children',
        ),
      ],
      total: 2,
      limit: 200,
      offset: 0,
      has_more: false,
    },
  });
}

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
  render(<DetailScreen />, {
    wrapper: createPlaybackWrapper(createTestQueryClient({ mutations: true })),
  });
}

beforeEach(async () => {
  runSignOutCleanups();
  mockParams = {};
  await signInTestUser();
  replyWithCatalogue();
  jest.spyOn(console, 'warn').mockImplementation(() => {});
});

describe('DetailScreen after the resolver backfills a library result its sources', () => {
  it('lists the resolved album tracks instead of the library-only ones', async () => {
    renderDetail(libraryResult('album'));

    expect(await screen.findByText('Dreams')).toBeTruthy();
    expect(screen.queryByText('The Chain')).toBeNull();
  });

  it('shows the resolved artist top tracks instead of the library-only ones', async () => {
    renderDetail(libraryResult('artist'));
    fireEvent.press(await screen.findByTestId('detail-explore-discography'));

    expect(await screen.findByText('Dayvan Cowboy')).toBeTruthy();
    expect(screen.getByText('POPULAR TRACKS')).toBeTruthy();
    expect(screen.queryByText('Roygbiv')).toBeNull();
  });
});
