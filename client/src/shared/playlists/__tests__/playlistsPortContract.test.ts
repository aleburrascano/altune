import { asTrackId } from '@shared/api-client/ids';
import type { PlaylistResponse } from '@shared/api-client/playlistResponse';
import { supabase } from '@shared/auth/supabaseClient';
import { ApiError } from '@shared/failure/errors';

import { httpPlaylistsPort } from '@shared/playlists/adapters/http/httpPlaylistsPort';
import type { PlaylistsPort } from '@shared/playlists/ports/playlistsPort';
import { createMemoryPlaylistsPort } from '@shared/playlists/__tests__/memoryPlaylistsPort';

const { __http, fakeFetch } = require('@testing/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const NO_CONTENT_STATUS = 204;
const NOT_FOUND_STATUS = 404;
const PLAYLISTS_PATH = '/v1/playlists';
const PLAYLIST_PATH_PATTERN = /^\/v1\/playlists\/([^/]+)$/;
const TRACKS_PATH_PATTERN = /^\/v1\/playlists\/([^/]+)\/tracks(\/batch)?$/;

const t1 = asTrackId('t1');
const t2 = asTrackId('t2');

type FakeRow = { summary: PlaylistResponse; trackIds: string[] };

function bodyOf(init: RequestInit | undefined): Record<string, unknown> {
  return JSON.parse(String(init?.body ?? '{}')) as Record<string, unknown>;
}

function serveFakeServer(): PlaylistsPort {
  const rows = new Map<string, FakeRow>();
  let nextId = 1;
  const reply = (method: string, path: string, response: object) =>
    __http.replyOnce(`${method} ${path}`, response);
  const missing = (method: string, path: string) =>
    reply(method, path, { status: NOT_FOUND_STATUS, json: { detail: 'not found' } });
  const recount = (row: FakeRow) => {
    row.summary = { ...row.summary, track_count: row.trackIds.length };
  };

  global.fetch = ((url: string, init?: RequestInit) => {
    const [path = '', query = ''] = String(url)
      .replace(/^[a-z]+:\/\/[^/]+/, '')
      .split('?');
    const method = String(init?.method ?? 'GET').toUpperCase();
    const one = PLAYLIST_PATH_PATTERN.exec(path)?.[1];
    const tracksOf = TRACKS_PATH_PATTERN.exec(path);
    const row = rows.get(one ?? tracksOf?.[1] ?? '');

    if (path === PLAYLISTS_PATH && method === 'GET') {
      const params = new URLSearchParams(query);
      const limit = params.has('limit') ? Number(params.get('limit')) : rows.size;
      const offset = Number(params.get('offset') ?? 0);
      const all = [...rows.values()].map((r) => r.summary);
      reply('GET', path, { json: { items: all.slice(offset, offset + limit), total: all.length } });
    } else if (path === PLAYLISTS_PATH && method === 'POST') {
      const id = `p${nextId++}`;
      const now = '2026-01-01T00:00:00Z';
      const summary = {
        id,
        name: String(bodyOf(init).name),
        track_count: 0,
        preview_artwork_urls: [],
        created_at: now,
        updated_at: now,
      } as unknown as PlaylistResponse;
      rows.set(id, { summary, trackIds: [] });
      reply('POST', path, { json: summary });
    } else if (one !== undefined && method === 'PATCH') {
      if (row === undefined) missing(method, path);
      else {
        row.summary = { ...row.summary, name: String(bodyOf(init).name) };
        reply(method, path, { json: row.summary });
      }
    } else if (one !== undefined && method === 'DELETE') {
      if (row === undefined) missing(method, path);
      else {
        rows.delete(one);
        reply(method, path, { status: NO_CONTENT_STATUS });
      }
    } else if (tracksOf !== null && method === 'POST') {
      if (row === undefined) missing(method, path);
      else {
        const requested = bodyOf(init).track_ids as string[];
        const fresh = requested.filter((id) => !row.trackIds.includes(id));
        row.trackIds.push(...fresh);
        recount(row);
        reply(method, path, {
          json: { added: fresh.length, skipped: requested.length - fresh.length },
        });
      }
    } else if (tracksOf !== null && method === 'DELETE') {
      if (row === undefined) missing(method, path);
      else {
        const gone = bodyOf(init).track_ids as string[];
        const before = row.trackIds.length;
        row.trackIds = row.trackIds.filter((id) => !gone.includes(id));
        recount(row);
        reply(method, path, { json: { removed: before - row.trackIds.length } });
      }
    }
    return fakeFetch(url, init);
  }) as typeof fetch;
  return httpPlaylistsPort;
}

function abortedSignal(): AbortSignal {
  const controller = new AbortController();
  controller.abort();
  return controller.signal;
}

beforeEach(() => {
  __http.reset();
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
});

afterEach(() => {
  global.fetch = fakeFetch;
});

describe.each<[string, () => PlaylistsPort]>([
  ['createMemoryPlaylistsPort', createMemoryPlaylistsPort],
  ['httpPlaylistsPort over HTTP', serveFakeServer],
])('%s satisfies the PlaylistsPort contract', (_name, freshPort) => {
  it('lists no playlists and a total of 0 for an empty store', async () => {
    const page = await freshPort().getPlaylists();

    expect(page.items).toEqual([]);
    expect(page.total).toBe(0);
  });

  it('lists a created playlist with a track_count of 0', async () => {
    const port = freshPort();

    await port.createPlaylist({ name: 'a' });

    const page = await port.getPlaylists();
    expect(page.items).toHaveLength(1);
    expect(page.items[0]).toMatchObject({ name: 'a', track_count: 0 });
  });

  it('adds new tracks and refuses a track already in the playlist', async () => {
    const port = freshPort();
    const { id } = await port.createPlaylist({ name: 'a' });

    const first = await port.addTracksToPlaylist(id, { track_ids: [t1, t2] });
    const again = await port.addTracksToPlaylist(id, { track_ids: [t1] });

    expect(first.added).toBe(2);
    expect(again.added).toBe(0);
    expect((await port.getPlaylists()).items[0]?.track_count).toBe(2);
  });

  it('lowers track_count by 1 when a track is removed', async () => {
    const port = freshPort();
    const { id } = await port.createPlaylist({ name: 'a' });
    await port.addTracksToPlaylist(id, { track_ids: [t1, t2] });

    await port.removeTracksFromPlaylist(id, { track_ids: [t1] });

    expect((await port.getPlaylists()).items[0]?.track_count).toBe(1);
  });

  it('lists the new name after a rename', async () => {
    const port = freshPort();
    const { id } = await port.createPlaylist({ name: 'a' });

    await port.renamePlaylist(id, 'b');

    expect((await port.getPlaylists()).items[0]?.name).toBe('b');
  });

  it('drops a deleted playlist and rejects a second delete with a 404 ApiError', async () => {
    const port = freshPort();
    const { id } = await port.createPlaylist({ name: 'a' });

    await port.deletePlaylist(id);

    expect((await port.getPlaylists()).items).toEqual([]);
    const second = port.deletePlaylist(id);
    await expect(second).rejects.toBeInstanceOf(ApiError);
    await expect(second).rejects.toMatchObject({ status: NOT_FOUND_STATUS });
  });

  it('serves one playlist when limit is 1 over 2 playlists', async () => {
    const port = freshPort();
    await port.createPlaylist({ name: 'a' });
    await port.createPlaylist({ name: 'b' });

    const page = await port.getPlaylists({ limit: 1 });

    expect(page.items).toHaveLength(1);
  });

  it('rejects getPlaylists with an AbortError when the signal is already aborted', async () => {
    const fetching = freshPort().getPlaylists({}, abortedSignal());

    await expect(fetching).rejects.toMatchObject({ name: 'AbortError' });
  });
});
