import { asTrackId } from '@shared/apiClient/ids';
import type {
  CreateTrackRequest,
  ListTracksResponse,
  TrackResponse,
} from '@shared/apiClient/trackResponse';

import { httpDetailPort } from '@features/detail/adapters/http/httpDetailPort';
import { createMemoryDetailPort } from '@features/detail/__tests__/memoryDetailPort';
import type { DetailPort } from '@features/detail/ports/detailPort';
import { signInTestUser } from '@testing/queryHarness';

const { __http, fakeFetch } = require('../../../../jest/doubles/fetch.js');

const TRACKS_PATH = '/v1/tracks';
const PAGE_LIMIT = 10;

type RecordedRequest = {
  path: string;
  method: string;
  headers: Record<string, string>;
};

function newTrack(title: string): CreateTrackRequest {
  return {
    title,
    artist: 'An Artist',
    album: null,
    duration_seconds: 180,
    artwork_url: null,
    isrc: null,
    year: null,
    genre: null,
    album_artist: null,
    track_number: null,
  };
}

function storedTrack(body: CreateTrackRequest, id: string): TrackResponse {
  return {
    ...body,
    id: asTrackId(id),
    added_at: '2024-01-01T00:00:00Z',
    acquisition_status: 'pending',
  } as TrackResponse;
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

function serveDetailOverHttp(): DetailPort {
  const tracks: TrackResponse[] = [];
  global.fetch = ((url: string, init?: RequestInit) => {
    const [path = '', query = ''] = String(url)
      .replace(/^[a-z]+:\/\/[^/]+/, '')
      .split('?');
    const method = String(init?.method ?? 'GET').toUpperCase();
    if (method === 'GET' && path === TRACKS_PATH) {
      __http.replyOnce(`GET ${TRACKS_PATH}`, {
        json: wirePage(tracks, new URLSearchParams(query)),
      });
    }
    if (method === 'POST' && path === TRACKS_PATH) {
      const created = storedTrack(JSON.parse(String(init?.body)), `created-${tracks.length + 1}`);
      tracks.push(created);
      __http.replyOnce(`POST ${TRACKS_PATH}`, { status: 201, json: created });
    }
    return fakeFetch(url, init);
  }) as typeof fetch;
  return httpDetailPort;
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
  __http.reset();
});

const SIGNAL_TAKING: [string, (port: DetailPort, signal: AbortSignal) => Promise<unknown>][] = [
  ['searchDiscovery', (port, signal) => port.searchDiscovery({ q: 'x' }, signal)],
  ['getEnrichment', (port, signal) => port.getEnrichment({ kind: 'album', title: 'x', signal })],
  [
    'getLastFmEnrichment',
    (port, signal) => port.getLastFmEnrichment({ kind: 'album', title: 'x', signal }),
  ],
  [
    'getDeezerEnrichment',
    (port, signal) => port.getDeezerEnrichment({ kind: 'album', title: 'x', signal }),
  ],
  [
    'getAlbumTracks',
    (port, signal) => port.getAlbumTracks({ provider: 'p', externalId: 'e', signal }),
  ],
  ['getRelatedTracks', (port, signal) => port.getRelatedTracks('p', 'e', undefined, signal)],
  ['getArtistContent', (port, signal) => port.getArtistContent('p', 'e', {}, signal)],
  ['getTracks', (port, signal) => port.getTracks({ limit: PAGE_LIMIT, offset: 0 }, signal)],
  ['getLibraryAlbums', (port, signal) => port.getLibraryAlbums({}, signal)],
];

describe.each<[string, () => DetailPort]>([
  ['createMemoryDetailPort', () => createMemoryDetailPort()],
  ['httpDetailPort over HTTP', serveDetailOverHttp],
])('%s satisfies the DetailPort contract', (_name, emptyPort) => {
  it('serves an empty first page from an empty library', async () => {
    const port = emptyPort();

    const page = await port.getTracks({ limit: PAGE_LIMIT, offset: 0 });

    expect(page.items).toEqual([]);
    expect(page).toMatchObject({ total: 0, has_more: false });
  });

  it('lists a created track on the next getTracks call, and a second one adds another', async () => {
    const port = emptyPort();

    await port.createTrack(newTrack('first'));
    const afterFirst = await port.getTracks({ limit: PAGE_LIMIT, offset: 0 });
    await port.createTrack(newTrack('second'));
    const afterSecond = await port.getTracks({ limit: PAGE_LIMIT, offset: 0 });

    expect(afterFirst.items.map((stored) => stored.title)).toEqual(['first']);
    expect(afterSecond.items.map((stored) => stored.title)).toEqual(['first', 'second']);
    expect(afterSecond.total).toBe(2);
  });

  it('serves an empty page when the offset is past the end', async () => {
    const port = emptyPort();
    await port.createTrack(newTrack('only'));

    const page = await port.getTracks({ limit: PAGE_LIMIT, offset: 5 });

    expect(page.items).toEqual([]);
    expect(page).toMatchObject({ total: 1, has_more: false });
  });

  it.each(SIGNAL_TAKING)(
    'rejects %s with an AbortError when the signal is already aborted',
    async (_member, call) => {
      const port = emptyPort();
      __http.replyAll({ json: {} });

      await expect(call(port, abortedSignal())).rejects.toMatchObject({ name: 'AbortError' });
    },
  );

  it.each(SIGNAL_TAKING)(
    'rejects %s with an AbortError when the signal aborts right after the call starts',
    async (_member, call) => {
      const port = emptyPort();
      __http.replyAll({ json: {} });
      const controller = new AbortController();

      const calling = call(port, controller.signal);
      controller.abort();

      await expect(calling).rejects.toMatchObject({ name: 'AbortError' });
    },
  );
});

describe('createMemoryDetailPort seeding', () => {
  it('answers has_content false for a title nobody seeded', async () => {
    const port = createMemoryDetailPort();

    const enrichment = await port.getEnrichment({ kind: 'album', title: 'Unknown' });

    expect(enrichment.has_content).toBe(false);
  });

  it('leaves the seed array untouched when a track is created', async () => {
    const seed: TrackResponse[] = [];
    const port = createMemoryDetailPort({ tracks: seed });

    await port.createTrack(newTrack('added'));

    expect(seed).toEqual([]);
  });
});

describe('httpDetailPort', () => {
  it('sends POST /v1/tracks with the given Idempotency-Key', async () => {
    const port = serveDetailOverHttp();

    await port.createTrack(newTrack('keyed'), 'k1');

    const sent = __http.last() as RecordedRequest;
    expect({ method: sent.method, path: sent.path }).toEqual({
      method: 'POST',
      path: TRACKS_PATH,
    });
    expect(sent.headers['Idempotency-Key']).toBe('k1');
  });
});
