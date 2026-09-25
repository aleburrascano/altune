import {
  getLibraryAlbums,
  getLibraryArtists,
  parseListAlbumsResponse,
  parseListArtistsResponse,
} from '../library';
import type {
  LibraryQuery,
  LibrarySort,
  ListAlbumsResponse,
  ListArtistsResponse,
} from '../library';
import { apiBase } from '../index';
import { supabase } from '@shared/auth/supabaseClient';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
});

function albumsPayload(): ListAlbumsResponse {
  return {
    items: [
      {
        key: 'a1',
        album: 'OK Computer',
        artist: 'Radiohead',
        artwork_url: null,
        year: 1997,
        track_count: 12,
        most_recent_added_at: '2024-01-01T00:00:00Z',
      },
    ],
    total: 1,
  };
}

function artistsPayload(): ListArtistsResponse {
  return {
    items: [
      {
        key: 'ar1',
        artist: 'Radiohead',
        artwork_url: null,
        track_count: 40,
        most_recent_added_at: '2024-01-01T00:00:00Z',
      },
    ],
    total: 1,
  };
}

describe('Table: libraryQueryString branches, reached through getLibraryAlbums', () => {
  const cases: [string, LibraryQuery, string][] = [
    ['neither q nor sort produces no query string at all (not a bare "?")', {}, ''],
    ['q only', { q: 'miles davis' }, '?q=miles+davis'],
    ['sort only', { sort: 'az' }, '?sort=az'],
    ['both q and sort', { q: 'miles', sort: 'year' }, '?q=miles&sort=year'],
    ['an empty-string q is falsy and is treated as absent, not sent as q=', { q: '' }, ''],
    ['limit only', { limit: 100 }, '?limit=100'],
    [
      'q, sort and limit',
      { q: 'miles', sort: 'recent', limit: 100 },
      '?q=miles&sort=recent&limit=100',
    ],
    ['a limit of 0 is sent, not dropped as falsy', { limit: 0 }, '?limit=0'],
    ['offset only', { offset: 50 }, '?offset=50'],
    ['an offset of 0 is sent, not dropped as falsy', { offset: 0 }, '?offset=0'],
    [
      'a page of a search: q, sort, limit and offset',
      { q: 'miles', sort: 'recent', limit: 50, offset: 50 },
      '?q=miles&sort=recent&limit=50&offset=50',
    ],
  ];

  it.each(cases)('%s', async (_label, query, expectedSuffix) => {
    __http.reply('GET /v1/library/albums', { status: 200, json: albumsPayload() });

    await getLibraryAlbums(query);

    expect(__http.last().url).toBe(`${apiBase}/v1/library/albums${expectedSuffix}`);
  });
});

describe('Table: libraryQueryString branches, reached through getLibraryArtists', () => {
  it.each([
    ['neither q nor sort produces no query string at all', {}, ''],
    ['both q and sort', { q: 'thom', sort: 'recent' as LibrarySort }, '?q=thom&sort=recent'],
  ])('%s', async (_label, query, expectedSuffix) => {
    __http.reply('GET /v1/library/artists', { status: 200, json: artistsPayload() });

    await getLibraryArtists(query);

    expect(__http.last().url).toBe(`${apiBase}/v1/library/artists${expectedSuffix}`);
  });
});

describe('getLibraryAlbums / getLibraryArtists default argument', () => {
  it('getLibraryAlbums called with no argument at all defaults to {} and returns the server payload untouched', async () => {
    const payload = albumsPayload();
    __http.reply('GET /v1/library/albums', { status: 200, json: payload });

    const result = await getLibraryAlbums();

    expect(__http.last().url).toBe(`${apiBase}/v1/library/albums`);
    expect(result).toEqual(payload);
  });

  it('getLibraryArtists called with no argument at all defaults to {} and returns the server payload untouched', async () => {
    const payload = artistsPayload();
    __http.reply('GET /v1/library/artists', { status: 200, json: payload });

    const result = await getLibraryArtists();

    expect(__http.last().url).toBe(`${apiBase}/v1/library/artists`);
    expect(result).toEqual(payload);
  });
});

// Regression test for #794.
describe('getLibraryAlbums / getLibraryArtists forward a caller abort signal', () => {
  it.each([
    ['getLibraryAlbums', 'GET /v1/library/albums', getLibraryAlbums],
    ['getLibraryArtists', 'GET /v1/library/artists', getLibraryArtists],
  ] as const)('%s aborts its in-flight request when the signal fires', async (_name, spec, fn) => {
    __http.hang(spec);
    const controller = new AbortController();

    const pending = fn({ q: 'radio' }, controller.signal);
    while (__http.requests.length === 0) await new Promise((r) => setTimeout(r, 0));
    controller.abort();

    await expect(pending).rejects.toThrow();
    expect(__http.last().signal.aborted).toBe(true);
  });
});

describe('wire parsing', () => {

  describe('parseListAlbumsResponse and parseListArtistsResponse', () => {
    it('parses an album group lens', () => {
      const albums = parseListAlbumsResponse({
        items: [
          {
            key: 'a1',
            album: 'OK Computer',
            artist: 'Radiohead',
            artwork_url: 'https://img/ok.png',
            year: 1997,
            track_count: 12,
            most_recent_added_at: '2024-01-01T00:00:00Z',
          },
        ],
        total: 1,
      });
      expect(albums.items[0]!.album).toBe('OK Computer');
    });

    it('parses an artist group lens', () => {
      const artists = parseListArtistsResponse({
        items: [
          {
            key: 'ar1',
            artist: 'Radiohead',
            artwork_url: null,
            track_count: 40,
            most_recent_added_at: '2024-01-01T00:00:00Z',
          },
        ],
        total: 1,
      });
      expect(artists.items[0]!.artist).toBe('Radiohead');
    });
  });
});
