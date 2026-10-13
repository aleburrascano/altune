import { asTrackId } from '@shared/apiClient/ids';
import type { ListTracksResponse, TrackResponse } from '@shared/apiClient/trackResponse';
import { MAX_ALL_TRACKS } from '@shared/apiClient/tracks';

import { httpLibraryPort } from '@features/library/adapters/http/httpLibraryPort';
import { createMemoryLibraryPort } from '@features/library/__tests__/memoryLibraryPort';
import { track } from '@features/library/__tests__/trackMutationFixtures';
import type { LibraryPort } from '@features/library/ports/libraryPort';
import { signInTestUser } from '@testing/signInTestUser';

const { __http, fakeFetch } = require('@testing/doubles/fetch.js');

const TRACKS_PATH = '/v1/tracks';
const TRACK_PATH_PATTERN = /^\/v1\/tracks\/([^/]+)$/;
const NO_CONTENT_STATUS = 204;
const SEEDED_TRACK_COUNT = 5;
const WHOLE_LIBRARY_TRACK_COUNT = 450;
const PAGE_LIMIT = 2;
const PAGE_OFFSET = 2;

type RecordedRequest = { path: string; method: string; query: string };

function tracksNumbered(count: number): TrackResponse[] {
  return Array.from({ length: count }, (_unused, index) => track(`t${index}`));
}

function wirePage(tracks: readonly TrackResponse[], query: URLSearchParams): ListTracksResponse {
  const limit = Number(query.get('limit'));
  const offset = Number(query.get('offset'));
  const items = tracks.slice(offset, offset + limit);
  return {
    items,
    total: tracks.length,
    limit,
    offset,
    has_more: offset + items.length < tracks.length,
  };
}

function serveLibraryOverHttp(seeded: readonly TrackResponse[]): LibraryPort {
  let tracks = [...seeded];
  global.fetch = ((url: string, init?: RequestInit) => {
    const [path = '', query = ''] = String(url)
      .replace(/^[a-z]+:\/\/[^/]+/, '')
      .split('?');
    const method = String(init?.method ?? 'GET').toUpperCase();
    const deletedId = TRACK_PATH_PATTERN.exec(path)?.[1];
    if (method === 'GET' && path === TRACKS_PATH) {
      __http.replyOnce(`GET ${TRACKS_PATH}`, {
        json: wirePage(tracks, new URLSearchParams(query)),
      });
    }
    if (method === 'DELETE' && deletedId !== undefined) {
      tracks = tracks.filter((stored) => stored.id !== deletedId);
      __http.replyOnce(`DELETE ${path}`, { status: NO_CONTENT_STATUS });
    }
    return fakeFetch(url, init);
  }) as typeof fetch;
  return httpLibraryPort;
}

function abortedSignal(): AbortSignal {
  const controller = new AbortController();
  controller.abort();
  return controller.signal;
}

beforeEach(async () => {
  await signInTestUser();
});

afterEach(() => {
  global.fetch = fakeFetch;
});

describe.each<[string, (tracks: TrackResponse[]) => LibraryPort]>([
  ['createMemoryLibraryPort', (tracks) => createMemoryLibraryPort({ tracks })],
  ['httpLibraryPort over HTTP', serveLibraryOverHttp],
])('%s satisfies the LibraryPort contract', (_name, seededPort) => {
  it('serves the tracks inside the requested limit and offset', async () => {
    const port = seededPort(tracksNumbered(SEEDED_TRACK_COUNT));

    const page = await port.getTracks({ limit: PAGE_LIMIT, offset: PAGE_OFFSET });

    expect(page.items.map((served) => served.id)).toEqual(['t2', 't3']);
    expect(page).toMatchObject({
      total: SEEDED_TRACK_COUNT,
      offset: PAGE_OFFSET,
      has_more: true,
    });
  });

  it('reports no more tracks on the page that reaches the end', async () => {
    const port = seededPort(tracksNumbered(SEEDED_TRACK_COUNT));

    const page = await port.getTracks({ limit: PAGE_LIMIT, offset: 4 });

    expect(page.items.map((served) => served.id)).toEqual(['t4']);
    expect(page.has_more).toBe(false);
  });

  it('returns every track of a 450-track library untruncated', async () => {
    const port = seededPort(tracksNumbered(WHOLE_LIBRARY_TRACK_COUNT));

    const whole = await port.getAllTracks({});

    expect(whole.items).toHaveLength(WHOLE_LIBRARY_TRACK_COUNT);
    expect(whole.truncated).toBe(false);
  });

  it('rejects a page fetch with an AbortError when the signal is already aborted', async () => {
    const port = seededPort(tracksNumbered(SEEDED_TRACK_COUNT));

    const fetching = port.getTracks({ limit: PAGE_LIMIT, offset: 0 }, abortedSignal());

    await expect(fetching).rejects.toMatchObject({ name: 'AbortError' });
  });

  it('rejects the whole-library fetch with an AbortError when the signal is already aborted', async () => {
    const port = seededPort(tracksNumbered(SEEDED_TRACK_COUNT));

    const fetching = port.getAllTracks({ signal: abortedSignal() });

    await expect(fetching).rejects.toMatchObject({ name: 'AbortError' });
  });

  it('no longer lists a track once it is deleted', async () => {
    const port = seededPort(tracksNumbered(SEEDED_TRACK_COUNT));

    await port.deleteTrack(asTrackId('t1'));

    const remaining = await port.getAllTracks({});
    expect(remaining.items.map((stored) => stored.id)).toEqual(['t0', 't2', 't3', 't4']);
  });
});

describe('httpLibraryPort', () => {
  it('deletes a track by sending DELETE /v1/tracks/<id>', async () => {
    const port = serveLibraryOverHttp(tracksNumbered(SEEDED_TRACK_COUNT));

    await port.deleteTrack(asTrackId('t1'));

    const sent = __http.last() as RecordedRequest;
    expect({ method: sent.method, path: sent.path }).toEqual({
      method: 'DELETE',
      path: '/v1/tracks/t1',
    });
  });
});

describe.each<[string, (tracks: TrackResponse[]) => LibraryPort]>([
  ['createMemoryLibraryPort', (tracks) => createMemoryLibraryPort({ tracks })],
  ['httpLibraryPort over HTTP', serveLibraryOverHttp],
])('%s at the edges of the LibraryPort contract', (_name, seededPort) => {
  it('serves an empty last page when the offset is past the end of the library', async () => {
    const port = seededPort(tracksNumbered(SEEDED_TRACK_COUNT));

    const page = await port.getTracks({ limit: PAGE_LIMIT, offset: 10 });

    expect(page.items).toEqual([]);
    expect(page).toMatchObject({ total: SEEDED_TRACK_COUNT, has_more: false });
  });

  it('serves an empty first page from an empty library', async () => {
    const port = seededPort([]);

    const page = await port.getTracks({ limit: PAGE_LIMIT, offset: 0 });

    expect(page.items).toEqual([]);
    expect(page).toMatchObject({ total: 0, has_more: false });
  });

  it('returns no tracks, untruncated, for the whole of an empty library', async () => {
    const port = seededPort([]);

    const whole = await port.getAllTracks({});

    expect(whole).toEqual({ items: [], truncated: false });
  });

  it('returns a library larger than one server page whole and in order', async () => {
    const port = seededPort(tracksNumbered(4500));

    const whole = await port.getAllTracks({});

    expect(whole.items).toHaveLength(4500);
    expect(whole.items[0]?.id).toBe('t0');
    expect(whole.items[2000]?.id).toBe('t2000');
    expect(whole.items[4499]?.id).toBe('t4499');
    expect(whole.truncated).toBe(false);
  });

  it('pages correctly when getTracks is called detached from the port', async () => {
    const { getTracks, getAllTracks } = seededPort(tracksNumbered(SEEDED_TRACK_COUNT));

    const page = await getTracks({ limit: PAGE_LIMIT, offset: 0 });
    const whole = await getAllTracks({});

    expect(page.items.map((served) => served.id)).toEqual(['t0', 't1']);
    expect(whole.items).toHaveLength(SEEDED_TRACK_COUNT);
  });

  it.each<[string, (port: LibraryPort, signal: AbortSignal) => Promise<unknown>]>([
    ['getLibraryAlbums', (port, signal) => port.getLibraryAlbums({}, signal)],
    ['getLibraryArtists', (port, signal) => port.getLibraryArtists({}, signal)],
    ['getPlaylists', (port, signal) => port.getPlaylists({}, signal)],
    ['searchDiscovery', (port, signal) => port.searchDiscovery({ q: 'x' }, signal)],
  ])('rejects %s with an AbortError when the signal is already aborted', async (_m, call) => {
    const port = seededPort(tracksNumbered(SEEDED_TRACK_COUNT));
    __http.replyAll({ json: {} });

    await expect(call(port, abortedSignal())).rejects.toMatchObject({ name: 'AbortError' });
  });

  it.each<[string, (port: LibraryPort, signal: AbortSignal) => Promise<unknown>]>([
    ['getTracks', (port, signal) => port.getTracks({ limit: PAGE_LIMIT, offset: 0 }, signal)],
    ['getAllTracks', (port, signal) => port.getAllTracks({ signal })],
    ['getLibraryAlbums', (port, signal) => port.getLibraryAlbums({}, signal)],
    ['getLibraryArtists', (port, signal) => port.getLibraryArtists({}, signal)],
    ['getPlaylists', (port, signal) => port.getPlaylists({}, signal)],
    ['searchDiscovery', (port, signal) => port.searchDiscovery({ q: 'x' }, signal)],
  ])(
    'rejects %s with an AbortError when the signal aborts right after the call starts',
    async (_m, call) => {
      const port = seededPort(tracksNumbered(SEEDED_TRACK_COUNT));
      __http.replyAll({ json: {} });
      const controller = new AbortController();

      const calling = call(port, controller.signal);
      controller.abort();

      await expect(calling).rejects.toMatchObject({ name: 'AbortError' });
    },
  );

  it('returns exactly the cap, truncated, for a library over MAX_ALL_TRACKS', async () => {
    const port = seededPort(tracksNumbered(MAX_ALL_TRACKS + 1));

    const whole = await port.getAllTracks({});

    expect(whole.items).toHaveLength(MAX_ALL_TRACKS);
    expect(whole.truncated).toBe(true);
  });

  it('rejects getPlaylist for a playlist that does not exist', async () => {
    const port = seededPort(tracksNumbered(SEEDED_TRACK_COUNT));
    __http.reply('GET /v1/playlists/missing', {
      status: 404,
      json: { error: { code: 'not_found', message: 'not found' } },
    });

    await expect(port.getPlaylist('missing' as never)).rejects.toBeInstanceOf(Error);
  });

  it('rejects deleting a track that is not in the library', async () => {
    const port = seededPort(tracksNumbered(SEEDED_TRACK_COUNT));
    __http.reply('DELETE /v1/tracks/missing', {
      status: 404,
      json: { error: { code: 'not_found', message: 'not found' } },
    });

    await expect(port.deleteTrack(asTrackId('missing'))).rejects.toBeInstanceOf(Error);
  });
});

describe('createMemoryLibraryPort seeding', () => {
  it('leaves the seed array untouched when a track is deleted', async () => {
    const seed = tracksNumbered(3);
    const port = createMemoryLibraryPort({ tracks: seed });

    await port.deleteTrack(asTrackId('t1'));

    expect(seed.map((seeded) => seeded.id)).toEqual(['t0', 't1', 't2']);
  });

  it('serves an empty library when created with no seed', async () => {
    const port = createMemoryLibraryPort();

    const whole = await port.getAllTracks({});

    expect(whole).toEqual({ items: [], truncated: false });
  });
});
