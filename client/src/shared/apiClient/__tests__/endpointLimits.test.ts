import {
  ENDPOINT_LIMITS,
  getAlbumTracks,
  getArtistContent,
  getPlaylists,
  getRelatedTracks,
  getTracks,
  listSearchHistory,
  searchDiscovery,
  suggestDiscovery,
} from '@shared/apiClient';
import { signedInWith } from '@shared/apiClient/__tests__/sessionLookup';
import { wireFixture } from '@testing/wireFixtures';

const { __http } = require('@testing/doubles/fetch.js');

beforeEach(() => {
  signedInWith();
});

const ABSURD = 1_000_000;

const CALLS = [
  {
    name: 'search',
    route: 'GET /v1/discovery/search',
    param: 'limit',
    max: ENDPOINT_LIMITS.search.max,
    send: () => searchDiscovery({ q: 'rad', limit: ABSURD }),
  },
  {
    name: 'artist content tracks',
    route: 'GET /v1/discovery/artists/spotify/ar-1/content',
    param: 'tracks_limit',
    max: ENDPOINT_LIMITS.artist_content_tracks.max,
    send: () => getArtistContent('spotify', 'ar-1', { tracksLimit: ABSURD }),
  },
  {
    name: 'artist content albums',
    route: 'GET /v1/discovery/artists/spotify/ar-1/content',
    param: 'albums_limit',
    max: ENDPOINT_LIMITS.artist_content_albums.max,
    send: () => getArtistContent('spotify', 'ar-1', { albumsLimit: ABSURD }),
  },
  {
    name: 'suggest',
    route: 'GET /v1/discovery/suggest',
    max: ENDPOINT_LIMITS.suggest.max,
    send: () => suggestDiscovery({ q: 'rad', limit: ABSURD }),
  },
  {
    name: 'search history',
    route: 'GET /v1/discovery/search-history',
    max: ENDPOINT_LIMITS.search_history.max,
    send: () => listSearchHistory({ limit: ABSURD }),
  },
  {
    name: 'related tracks',
    route: 'GET /v1/discovery/tracks/soundcloud/sc-1/related',
    max: ENDPOINT_LIMITS.related_tracks.max,
    send: () => getRelatedTracks('soundcloud', 'sc-1', ABSURD),
  },
  {
    name: 'album tracks',
    route: 'GET /v1/discovery/albums/musicbrainz/mb-1/tracks',
    max: ENDPOINT_LIMITS.album_tracks.max,
    send: () => getAlbumTracks({ provider: 'musicbrainz', externalId: 'mb-1', limit: ABSURD }),
  },
  {
    name: 'library tracks',
    route: 'GET /v1/tracks',
    max: ENDPOINT_LIMITS.library_page.max,
    send: () => getTracks({ limit: ABSURD, offset: 0 }),
  },
  {
    name: 'playlists',
    route: 'GET /v1/playlists',
    max: ENDPOINT_LIMITS.library_page.max,
    send: () => getPlaylists({ limit: ABSURD }),
  },
];

describe('the limits the client may send match the server contract', () => {
  it('has the same numbers as contracts/wire/endpoint_limits.json, which the go-api checks too', () => {
    expect(ENDPOINT_LIMITS).toEqual(wireFixture('endpoint_limits'));
  });
});

describe('a limit above the server maximum is sent as the maximum', () => {
  it.each(CALLS)('$name', async ({ route, max, send, param = 'limit' }) => {
    __http.reply(route, { status: 400, json: { error: { code: 'x', message: 'x' } } });

    await expect(send()).rejects.toBeDefined();

    expect(new URLSearchParams(__http.last().query).get(param)).toBe(String(max));
  });

  it('leaves a limit below the maximum alone', async () => {
    __http.reply('GET /v1/discovery/suggest', { status: 400, json: { error: { code: 'x' } } });

    await expect(suggestDiscovery({ q: 'rad', limit: 3 })).rejects.toBeDefined();

    expect(new URLSearchParams(__http.last().query).get('limit')).toBe('3');
  });
});
