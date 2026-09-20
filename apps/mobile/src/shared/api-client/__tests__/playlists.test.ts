import {
  addTracksToPlaylist,
  createPlaylist,
  deletePlaylist,
  getPlaylist,
  getPlaylists,
  removeTracksFromPlaylist,
  renamePlaylist,
  reorderPlaylistTracks,
  type PlaylistPage,
} from '../playlists';
import { apiBase } from '../index';
import { ApiError, ContractError, NetworkError } from '@shared/errors';
import { supabase } from '@shared/auth/supabaseClient';
import { asPlaylistId, asTrackId, parsePlaylistId } from '@shared/api-client/ids';
import type { PlaylistId } from '../ids';
import type { PlaylistDetailResponse, PlaylistResponse, TrackResponse } from '../types';

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

function playlist(overrides: Partial<PlaylistResponse> = {}): PlaylistResponse {
  return {
    id: asPlaylistId('p1'),
    name: 'Focus',
    track_count: 0,
    preview_artwork_urls: [],
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    ...overrides,
  };
}

function track(overrides: Partial<TrackResponse> = {}): TrackResponse {
  return {
    id: asTrackId('t1'),
    title: 'Kid A',
    artist: 'Radiohead',
    album: null,
    duration_seconds: 240,
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
    ...overrides,
  } as TrackResponse;
}

function detail(overrides: Partial<PlaylistDetailResponse> = {}): PlaylistDetailResponse {
  return { ...playlist(), total_duration_seconds: 0, tracks: [], ...overrides };
}

describe('getPlaylists', () => {
  it('GETs the collection endpoint and resolves the items', async () => {
    __http.reply('GET /v1/playlists', {
      status: 200,
      json: { items: [playlist()], total: 1 },
    });

    await expect(getPlaylists()).resolves.toEqual({ items: [playlist()], total: 1 });
    expect(__http.last().method).toBe('GET');
    expect(__http.last().path).toBe('/v1/playlists');
  });

  it.each<[string, PlaylistPage, string]>([
    ['no page produces no query string at all (not a bare "?")', {}, ''],
    ['limit only', { limit: 50 }, '?limit=50'],
    ['offset only', { offset: 50 }, '?offset=50'],
    ['limit and offset', { limit: 50, offset: 100 }, '?limit=50&offset=100'],
    ['a limit of 0 is sent, not dropped as falsy', { limit: 0 }, '?limit=0'],
    ['an offset of 0 is sent, not dropped as falsy', { offset: 0 }, '?offset=0'],
  ])('%s', async (_label, page, expectedSuffix) => {
    __http.reply('GET /v1/playlists', { status: 200, json: { items: [], total: 0 } });

    await getPlaylists(page);

    expect(__http.last().url).toBe(`${apiBase}/v1/playlists${expectedSuffix}`);
  });

  it('rejects with a ContractError naming the field when an item drifts from the contract', async () => {
    __http.reply('GET /v1/playlists', {
      status: 200,
      json: { items: [{ id: 'p1', name: 'Focus' }], total: 1 },
    });

    const error = await getPlaylists().catch((e: unknown) => e);

    expect(error).toBeInstanceOf(ContractError);
    expect((error as ContractError).at).toBe('ListPlaylistsResponse.items[0].track_count');
  });

  it('rejects with a ContractError when items is null', async () => {
    __http.reply('GET /v1/playlists', { status: 200, json: { items: null, total: 0 } });

    await expect(getPlaylists()).rejects.toMatchObject({
      name: 'ContractError',
      at: 'ListPlaylistsResponse.items',
    });
  });

  it('surfaces a transport failure as NetworkError', async () => {
    __http.fail('GET /v1/playlists');

    await expect(getPlaylists()).rejects.toBeInstanceOf(NetworkError);
  });
});

describe('getPlaylist', () => {
  it('GETs the interpolated playlist id', async () => {
    __http.reply('GET /v1/playlists/p1', {
      status: 200,
      json: detail(),
    });

    await getPlaylist(asPlaylistId('p1'));

    expect(__http.last().method).toBe('GET');
    expect(__http.last().path).toBe('/v1/playlists/p1');
  });

  it('resolves a well-formed detail response with its tracks parsed', async () => {
    const body = detail({ track_count: 1, total_duration_seconds: 240, tracks: [track()] });
    __http.reply('GET /v1/playlists/p1', { status: 200, json: body });

    await expect(getPlaylist(asPlaylistId('p1'))).resolves.toEqual(body);
  });

  it('rejects with a ContractError when tracks is missing, instead of handing render an undefined list', async () => {
    __http.reply('GET /v1/playlists/p1', {
      status: 200,
      json: { ...detail(), tracks: undefined },
    });

    await expect(getPlaylist(asPlaylistId('p1'))).rejects.toMatchObject({
      name: 'ContractError',
      at: 'PlaylistDetailResponse.tracks',
    });
  });

  it('rejects with a ContractError when a nested track drifts from the contract', async () => {
    __http.reply('GET /v1/playlists/p1', {
      status: 200,
      json: detail({ tracks: [{ ...track(), title: 42 } as unknown as TrackResponse] }),
    });

    await expect(getPlaylist(asPlaylistId('p1'))).rejects.toMatchObject({
      name: 'ContractError',
      at: 'PlaylistDetailResponse.tracks[0].title',
    });
  });

  it('rejects a null body with a ContractError rather than resolving null', async () => {
    __http.reply('GET /v1/playlists/p1', { status: 200, json: null });

    await expect(getPlaylist(asPlaylistId('p1'))).rejects.toBeInstanceOf(ContractError);
  });

  it('rejects with a ContractError when preview_artwork_urls holds a non-string', async () => {
    __http.reply('GET /v1/playlists/p1', {
      status: 200,
      json: { ...detail(), preview_artwork_urls: [null] },
    });

    await expect(getPlaylist(asPlaylistId('p1'))).rejects.toMatchObject({
      name: 'ContractError',
      at: 'PlaylistDetailResponse.preview_artwork_urls[0]',
    });
  });
});

describe('createPlaylist', () => {
  it('POSTs the name with a JSON content-type', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: playlist() });

    await expect(createPlaylist({ name: 'Focus' })).resolves.toEqual(playlist());

    const request = __http.last();
    expect(request.method).toBe('POST');
    expect(request.path).toBe('/v1/playlists');
    expect(request.headers['Content-Type']).toBe('application/json');
    expect(JSON.parse(request.body)).toEqual({ name: 'Focus' });
  });

  it('rejects with a ContractError when the created playlist lacks an id', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: { ...playlist(), id: undefined } });

    await expect(createPlaylist({ name: 'Focus' })).rejects.toMatchObject({
      name: 'ContractError',
      at: 'PlaylistResponse.id',
    });
  });
});

describe('renamePlaylist', () => {
  it('PATCHes the exact playlist id with a { name } body', async () => {
    __http.reply('PATCH /v1/playlists/p1', { status: 200, json: playlist({ name: 'New name' }) });

    await expect(renamePlaylist(asPlaylistId('p1'), 'New name')).resolves.toEqual(
      playlist({ name: 'New name' }),
    );

    const request = __http.last();
    expect(request.method).toBe('PATCH');
    expect(request.path).toBe('/v1/playlists/p1');
    expect(request.headers['Content-Type']).toBe('application/json');
    expect(JSON.parse(request.body)).toEqual({ name: 'New name' });
  });

  it('rejects with a ContractError when the renamed playlist has an off-type name', async () => {
    __http.reply('PATCH /v1/playlists/p1', {
      status: 200,
      json: { ...playlist(), name: null },
    });

    await expect(renamePlaylist(asPlaylistId('p1'), 'New name')).rejects.toMatchObject({
      name: 'ContractError',
      at: 'PlaylistResponse.name',
    });
  });

  it('rejects with ApiError(404) when renaming a Playlist that no longer exists, so the caller can roll its optimistic edit back', async () => {
    __http.reply('PATCH /v1/playlists/deleted-playlist', {
      status: 404,
      json: { message: 'not found' },
    });

    await expect(
      renamePlaylist(asPlaylistId('deleted-playlist'), 'New name'),
    ).rejects.toBeInstanceOf(ApiError);
    await expect(
      renamePlaylist(asPlaylistId('deleted-playlist'), 'New name'),
    ).rejects.toMatchObject({
      status: 404,
    });
  });
});

describe('deletePlaylist', () => {
  it('DELETEs the exact playlist path and resolves void on 204', async () => {
    __http.reply('DELETE /v1/playlists/p1', { status: 204 });

    await expect(deletePlaylist(asPlaylistId('p1'))).resolves.toBeUndefined();
    expect(__http.last().method).toBe('DELETE');
    expect(__http.last().path).toBe('/v1/playlists/p1');
  });
});

describe('addTracksToPlaylist', () => {
  it('POSTs the whole id list to the batch endpoint in one request', async () => {
    __http.reply('POST /v1/playlists/p1/tracks/batch', {
      status: 200,
      json: { added: 2, skipped: 1 },
    });

    const result = await addTracksToPlaylist(asPlaylistId('p1'), {
      track_ids: [asTrackId('t1'), asTrackId('t2'), asTrackId('t3')],
    });

    const request = __http.last();
    expect(request.method).toBe('POST');
    expect(request.path).toBe('/v1/playlists/p1/tracks/batch');
    expect(request.headers['Content-Type']).toBe('application/json');
    expect(JSON.parse(request.body)).toEqual({ track_ids: ['t1', 't2', 't3'] });
    expect(__http.countFor('POST /v1/playlists/p1/tracks/batch')).toBe(1);
    expect(result).toEqual({ added: 2, skipped: 1 });
  });

  it('reports the server verdict rather than assuming every requested track landed', async () => {
    __http.reply('POST /v1/playlists/p1/tracks/batch', {
      status: 200,
      json: { added: 0, skipped: 2 },
    });

    await expect(
      addTracksToPlaylist(asPlaylistId('p1'), { track_ids: [asTrackId('t1'), asTrackId('t2')] }),
    ).resolves.toEqual({
      added: 0,
      skipped: 2,
    });
  });

  it.each([
    ['added is missing', { skipped: 1 }, 'AddTracksToPlaylistResponse.added'],
    ['skipped is missing', { added: 1 }, 'AddTracksToPlaylistResponse.skipped'],
    ['added is a string', { added: '1', skipped: 0 }, 'AddTracksToPlaylistResponse.added'],
    ['added is fractional', { added: 1.5, skipped: 0 }, 'AddTracksToPlaylistResponse.added'],
    ['added is negative', { added: -1, skipped: 0 }, 'AddTracksToPlaylistResponse.added'],
  ])(
    'rejects with a ContractError naming the count when %s, rather than handing arithmetic an undefined',
    async (_label, json, at) => {
      __http.reply('POST /v1/playlists/p1/tracks/batch', { status: 200, json });

      await expect(
        addTracksToPlaylist(asPlaylistId('p1'), { track_ids: [asTrackId('t1')] }),
      ).rejects.toMatchObject({ name: 'ContractError', at });
    },
  );

  it('rejects a null body with a ContractError rather than resolving null', async () => {
    __http.reply('POST /v1/playlists/p1/tracks/batch', { status: 200, json: null });

    await expect(
      addTracksToPlaylist(asPlaylistId('p1'), { track_ids: [asTrackId('t1')] }),
    ).rejects.toBeInstanceOf(ContractError);
  });
});

describe('removeTracksFromPlaylist', () => {
  it('DELETEs the tracks collection with the id list in the body', async () => {
    __http.reply('DELETE /v1/playlists/p1/tracks', { status: 200, json: { removed: 2 } });

    const result = await removeTracksFromPlaylist(asPlaylistId('p1'), {
      track_ids: [asTrackId('t1'), asTrackId('t2')],
    });

    const request = __http.last();
    expect(request.method).toBe('DELETE');
    expect(request.path).toBe('/v1/playlists/p1/tracks');
    expect(request.headers['Content-Type']).toBe('application/json');
    expect(JSON.parse(request.body)).toEqual({ track_ids: ['t1', 't2'] });
    expect(result).toEqual({ removed: 2 });
  });

  it.each([
    ['removed is missing', {}],
    ['removed is null', { removed: null }],
    ['removed is negative', { removed: -2 }],
  ])('rejects with a ContractError naming the count when %s', async (_label, json) => {
    __http.reply('DELETE /v1/playlists/p1/tracks', { status: 200, json });

    await expect(
      removeTracksFromPlaylist(asPlaylistId('p1'), { track_ids: [asTrackId('t1')] }),
    ).rejects.toMatchObject({
      name: 'ContractError',
      at: 'RemoveTracksFromPlaylistResponse.removed',
    });
  });

  it('addresses exactly that Playlist, and nothing else', async () => {
    __http.reply('DELETE /v1/playlists/p1/tracks', { status: 200, json: { removed: 1 } });
    __http.reply('DELETE /v1/playlists/t1/tracks', { status: 500 });

    await removeTracksFromPlaylist(asPlaylistId('p1'), { track_ids: [asTrackId('t1')] });

    expect(__http.countFor('DELETE /v1/playlists/t1/tracks')).toBe(0);
  });
});

describe('reorderPlaylistTracks', () => {
  it('PATCHes the reorder endpoint with the full ordered track_ids list, not a delta', async () => {
    __http.reply('PATCH /v1/playlists/p1/tracks/reorder', { status: 204 });
    const order = [asTrackId('t3'), asTrackId('t1'), asTrackId('t2')];

    await reorderPlaylistTracks(asPlaylistId('p1'), { track_ids: order });

    const request = __http.last();
    expect(request.method).toBe('PATCH');
    expect(request.path).toBe('/v1/playlists/p1/tracks/reorder');
    expect(request.headers['Content-Type']).toBe('application/json');
    expect(JSON.parse(request.body)).toEqual({ track_ids: order });
  });
});

describe('playlist id path safety (#786)', () => {
  // Before #786 four of the six call sites interpolated the id raw and two escaped it. Escaping
  // is not enough on its own either: URL resolution collapses a `..` segment (even `%2e%2e`), so
  // `removeTracksFromPlaylist('..')` would have hit `DELETE /v1/tracks`. Every endpoint must
  // refuse such an id before sending anything. asPlaylistId itself now refuses them (#944), so
  // they are cast in here to prove idPathSegment still refuses an id smuggled past the brand.
  const track = [asTrackId('t1')];
  const endpoints = [
    ['getPlaylist', (id: PlaylistId) => getPlaylist(id)],
    ['renamePlaylist', (id: PlaylistId) => renamePlaylist(id, 'New name')],
    ['deletePlaylist', (id: PlaylistId) => deletePlaylist(id)],
    ['addTracksToPlaylist', (id: PlaylistId) => addTracksToPlaylist(id, { track_ids: track })],
    [
      'removeTracksFromPlaylist',
      (id: PlaylistId) => removeTracksFromPlaylist(id, { track_ids: track }),
    ],
    ['reorderPlaylistTracks', (id: PlaylistId) => reorderPlaylistTracks(id, { track_ids: track })],
  ] as const;
  const hostileIds = ['..', '%2e%2e', 'p1/tracks', 'p/1', 'p1?x=1', 'p1#frag', ''];

  describe.each(endpoints)('%s', (_name, call) => {
    it.each(hostileIds)('refuses the id %p without sending a request', async (id) => {
      __http.replyAll({ status: 200, json: {} });

      await expect(call(id as PlaylistId)).rejects.toBeInstanceOf(ContractError);

      expect(__http.requests).toHaveLength(0);
    });
  });
});

describe('parsePlaylistId', () => {
  it('accepts a UUID and short opaque ids', () => {
    expect(parsePlaylistId('0b7c9a4e-1f2d-4c3b-9a8e-7d6f5e4c3b2a')).toEqual({
      ok: true,
      id: '0b7c9a4e-1f2d-4c3b-9a8e-7d6f5e4c3b2a',
    });
    expect(parsePlaylistId('p1').ok).toBe(true);
  });

  it.each(['', '../x', 'a/b', 'a?b', 'a#b', 'a%2Fb', 'a.b', 'a b', 'x'.repeat(129)])(
    'rejects %p with a typed failure',
    (value) => {
      expect(parsePlaylistId(value)).toEqual({
        ok: false,
        error: { kind: 'invalid-playlist-id', value },
      });
    },
  );
});
