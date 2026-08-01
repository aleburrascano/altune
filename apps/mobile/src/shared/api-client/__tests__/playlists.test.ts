import {
  addTracksToPlaylist,
  createPlaylist,
  deletePlaylist,
  getPlaylist,
  getPlaylists,
  removeTracksFromPlaylist,
  renamePlaylist,
  reorderPlaylistTracks,
} from '../playlists';
import { ApiError, NetworkError } from '../errors';
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

describe('getPlaylists', () => {
  it('GETs the collection endpoint and resolves the items', async () => {
    __http.reply('GET /v1/playlists', {
      status: 200,
      json: { items: [{ id: 'p1', name: 'Focus' }], total: 1 },
    });

    await expect(getPlaylists()).resolves.toEqual({
      items: [{ id: 'p1', name: 'Focus' }],
      total: 1,
    });
    expect(__http.last().method).toBe('GET');
    expect(__http.last().path).toBe('/v1/playlists');
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
      json: { id: 'p1', name: 'Focus', tracks: [], total_duration_seconds: 0 },
    });

    await getPlaylist('p1');

    expect(__http.last().method).toBe('GET');
    expect(__http.last().path).toBe('/v1/playlists/p1');
  });

  it('hands the caller a detail response missing tracks as-is, without inventing an empty list', async () => {
    __http.reply('GET /v1/playlists/p1', {
      status: 200,
      json: { id: 'p1', name: 'Focus', total_duration_seconds: 0 },
    });

    const result = await getPlaylist('p1');

    expect((result as { tracks?: unknown }).tracks).toBeUndefined();
  });

  it('hands the caller a null body as-is rather than throwing', async () => {
    __http.reply('GET /v1/playlists/p1', { status: 200, json: null });

    await expect(getPlaylist('p1')).resolves.toBeNull();
  });

  it('interpolates the id into the path unescaped, so a "/" is carried through as an extra path segment', async () => {
    __http.reply('GET /v1/playlists/p1/tracks', { status: 200, json: {} });

    await getPlaylist('p1/tracks');

    expect(__http.last().path).toBe('/v1/playlists/p1/tracks');
  });
});

describe('createPlaylist', () => {
  it('POSTs the name with a JSON content-type', async () => {
    __http.reply('POST /v1/playlists', { status: 201, json: { id: 'p1', name: 'Focus' } });

    await createPlaylist({ name: 'Focus' });

    const request = __http.last();
    expect(request.method).toBe('POST');
    expect(request.path).toBe('/v1/playlists');
    expect(request.headers['Content-Type']).toBe('application/json');
    expect(JSON.parse(request.body)).toEqual({ name: 'Focus' });
  });
});

describe('renamePlaylist', () => {
  it('PATCHes the exact playlist id with a { name } body', async () => {
    __http.reply('PATCH /v1/playlists/p1', { status: 200, json: { id: 'p1', name: 'New name' } });

    await renamePlaylist('p1', 'New name');

    const request = __http.last();
    expect(request.method).toBe('PATCH');
    expect(request.path).toBe('/v1/playlists/p1');
    expect(request.headers['Content-Type']).toBe('application/json');
    expect(JSON.parse(request.body)).toEqual({ name: 'New name' });
  });

  it('rejects with ApiError(404) when renaming a Playlist that no longer exists, so the caller can roll its optimistic edit back', async () => {
    __http.reply('PATCH /v1/playlists/deleted-playlist', {
      status: 404,
      json: { message: 'not found' },
    });

    await expect(renamePlaylist('deleted-playlist', 'New name')).rejects.toBeInstanceOf(ApiError);
    await expect(renamePlaylist('deleted-playlist', 'New name')).rejects.toMatchObject({
      status: 404,
    });
  });
});

describe('deletePlaylist', () => {
  it('DELETEs the exact playlist path and resolves void on 204', async () => {
    __http.reply('DELETE /v1/playlists/p1', { status: 204 });

    await expect(deletePlaylist('p1')).resolves.toBeUndefined();
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

    const result = await addTracksToPlaylist('p1', { track_ids: ['t1', 't2', 't3'] });

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

    await expect(addTracksToPlaylist('p1', { track_ids: ['t1', 't2'] })).resolves.toEqual({
      added: 0,
      skipped: 2,
    });
  });

  it('escapes the playlist id so a "/" cannot forge an extra path segment', async () => {
    __http.reply('POST /v1/playlists/p%2F1/tracks/batch', { status: 200, json: { added: 1, skipped: 0 } });

    await addTracksToPlaylist('p/1', { track_ids: ['t1'] });

    expect(__http.last().path).toBe('/v1/playlists/p%2F1/tracks/batch');
  });
});

describe('removeTracksFromPlaylist', () => {
  it('DELETEs the tracks collection with the id list in the body', async () => {
    __http.reply('DELETE /v1/playlists/p1/tracks', { status: 200, json: { removed: 2 } });

    const result = await removeTracksFromPlaylist('p1', { track_ids: ['t1', 't2'] });

    const request = __http.last();
    expect(request.method).toBe('DELETE');
    expect(request.path).toBe('/v1/playlists/p1/tracks');
    expect(request.headers['Content-Type']).toBe('application/json');
    expect(JSON.parse(request.body)).toEqual({ track_ids: ['t1', 't2'] });
    expect(result).toEqual({ removed: 2 });
  });

  it('addresses exactly that Playlist, and nothing else', async () => {
    __http.reply('DELETE /v1/playlists/p1/tracks', { status: 200, json: { removed: 1 } });
    __http.reply('DELETE /v1/playlists/t1/tracks', { status: 500 });

    await removeTracksFromPlaylist('p1', { track_ids: ['t1'] });

    expect(__http.countFor('DELETE /v1/playlists/t1/tracks')).toBe(0);
  });
});

describe('reorderPlaylistTracks', () => {
  it('PATCHes the reorder endpoint with the full ordered track_ids list, not a delta', async () => {
    __http.reply('PATCH /v1/playlists/p1/tracks/reorder', { status: 204 });
    const order = ['t3', 't1', 't2'];

    await reorderPlaylistTracks('p1', { track_ids: order });

    const request = __http.last();
    expect(request.method).toBe('PATCH');
    expect(request.path).toBe('/v1/playlists/p1/tracks/reorder');
    expect(request.headers['Content-Type']).toBe('application/json');
    expect(JSON.parse(request.body)).toEqual({ track_ids: order });
  });
});
