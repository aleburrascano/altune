import { supabase } from '@shared/auth/supabaseClient';

import { httpOfflinePort } from '@shared/offline/adapters/http/httpOfflinePort';
import {
  createMemoryOfflinePort,
  type SeededAudioUrls,
} from '@shared/offline/__tests__/memoryOfflinePort';
import type { OfflinePort } from '@shared/offline/ports/offlinePort';

const { __http, fakeFetch } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const AUDIO_URLS_PATH = '/v1/audio-urls';
const T1_URL = 'https://cdn.example.com/t1.mp3?sig=a';

type RecordedRequest = { path: string; method: string; body: string };

function wireUrls(urls: SeededAudioUrls, body: unknown): unknown {
  const { track_ids: requested } = JSON.parse(String(body)) as { track_ids: string[] };
  return {
    urls: requested
      .filter((id) => Object.hasOwn(urls, id))
      .map((id) => ({ track_id: id, url: urls[id]?.url, version: urls[id]?.version })),
  };
}

function serveAudioUrlsOverHttp(urls: SeededAudioUrls): OfflinePort {
  global.fetch = ((url: string, init?: RequestInit) => {
    if (String(url).includes(AUDIO_URLS_PATH) && init?.method === 'POST') {
      __http.replyOnce(`POST ${AUDIO_URLS_PATH}`, { json: wireUrls(urls, init.body) });
    }
    return fakeFetch(url, init);
  }) as typeof fetch;
  return httpOfflinePort;
}

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
});

afterEach(() => {
  global.fetch = fakeFetch;
});

describe.each<[string, (urls: SeededAudioUrls) => OfflinePort]>([
  ['createMemoryOfflinePort', (urls) => createMemoryOfflinePort({ urls })],
  ['httpOfflinePort over HTTP', serveAudioUrlsOverHttp],
])('%s satisfies the OfflinePort contract', (name, portFor) => {
  it('resolves no urls for no track ids, sending nothing over the wire', async () => {
    const port = portFor({ t1: { url: T1_URL, version: 'v1' } });

    await expect(port.fetchAudioUrls([])).resolves.toEqual([]);

    if (name.startsWith('http')) expect(__http.requests).toEqual([]);
  });

  it('resolves the seeded url and version of one track id', async () => {
    const port = portFor({ t1: { url: T1_URL, version: 'v1' } });

    await expect(port.fetchAudioUrls(['t1'])).resolves.toEqual([
      { trackId: 't1', url: T1_URL, version: 'v1' },
    ]);
  });

  it('leaves out an id that only looks like a seeded one, without rejecting', async () => {
    const port = portFor({ t1: { url: T1_URL, version: 'v1' } });

    await expect(port.fetchAudioUrls(['t1x'])).resolves.toEqual([]);
    await expect(port.fetchAudioUrls(['t1x', 't1'])).resolves.toEqual([
      { trackId: 't1', url: T1_URL, version: 'v1' },
    ]);
  });

  it('serves the version the seed holds at the time of each call', async () => {
    const urls: SeededAudioUrls = { t1: { url: T1_URL, version: 'v1' } };
    const port = portFor(urls);

    const first = await port.fetchAudioUrls(['t1']);
    urls['t1'] = { url: T1_URL, version: 'v2' };
    const second = await port.fetchAudioUrls(['t1']);

    expect([first[0]?.version, second[0]?.version]).toEqual(['v1', 'v2']);
  });
});

describe('httpOfflinePort', () => {
  it('sends POST /v1/audio-urls with the requested ids as track_ids', async () => {
    const port = serveAudioUrlsOverHttp({ t1: { url: T1_URL, version: 'v1' } });

    await port.fetchAudioUrls(['t1']);

    const sent = __http.last() as RecordedRequest;
    expect({ method: sent.method, path: sent.path, body: JSON.parse(sent.body) }).toEqual({
      method: 'POST',
      path: AUDIO_URLS_PATH,
      body: { track_ids: ['t1'] },
    });
  });
});
