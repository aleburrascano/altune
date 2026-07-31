import { getLibraryAlbums, getLibraryArtists } from '../library';
import type { LibrarySort, ListAlbumsResponse, ListArtistsResponse } from '../library';
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
  const cases: [string, { q?: string; sort?: LibrarySort }, string][] = [
    ['neither q nor sort produces no query string at all (not a bare "?")', {}, ''],
    ['q only', { q: 'miles davis' }, '?q=miles+davis'],
    ['sort only', { sort: 'az' }, '?sort=az'],
    ['both q and sort', { q: 'miles', sort: 'year' }, '?q=miles&sort=year'],
    ['an empty-string q is falsy and is treated as absent, not sent as q=', { q: '' }, ''],
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
