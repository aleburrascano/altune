import { asTrackId } from '@shared/api-client/ids';
import type { QueueStateResponse } from '@shared/api-client/playback';
import { supabase } from '@shared/auth/supabaseClient';
import { ContractError } from '@shared/errors';

import { httpPlaybackPort } from '@features/playback/adapters/http/httpPlaybackPort';
import { createMemoryPlaybackPort } from '@features/playback/__tests__/memoryPlaybackPort';
import type { PlaybackPort } from '@features/playback/ports/playbackPort';

const { __http, fakeFetch } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const NOT_FOUND_STATUS = 404;
const NO_CONTENT_STATUS = 204;
const AUDIO_URLS_PATH = '/v1/audio-urls';
const QUEUE_PATH = '/v1/playback/queue-state';
const T1 = asTrackId('t1');
const T1_AUDIO = { url: 'https://cdn.test/t1.mp3', version: 'v1' };
const LYRICS = { plain: 'la la', synced_lines: [], writers: [], copyright: 'c' };

interface PlaybackWorld {
  prefetchEnabled?: boolean;
}

function queueState(trackIds: string[]): QueueStateResponse {
  return {
    track_ids: trackIds,
    current_index: 0,
    position_ms: 0,
    shuffled: false,
    repeat_mode: 'off',
    source: null,
    natural_order: trackIds,
  };
}

function servePlaybackOverHttp({ prefetchEnabled = true }: PlaybackWorld): PlaybackPort {
  let saved: unknown;
  global.fetch = ((url: string, init?: RequestInit) => {
    const path =
      String(url)
        .replace(/^[a-z]+:\/\/[^/]+/, '')
        .split('?')[0] ?? '';
    const method = String(init?.method ?? 'GET').toUpperCase();
    if (method === 'POST' && path === AUDIO_URLS_PATH) {
      __http.replyOnce(`POST ${path}`, {
        json: {
          urls: [{ track_id: 't1', ...T1_AUDIO }],
          prefetch_enabled: prefetchEnabled,
        },
      });
    } else if (method === 'PUT' && path === QUEUE_PATH) {
      saved = JSON.parse(String(init?.body));
      __http.replyOnce(`PUT ${path}`, { status: NO_CONTENT_STATUS });
    } else if (method === 'GET' && path === QUEUE_PATH) {
      __http.replyOnce(`GET ${path}`, { json: saved });
    } else if (path.startsWith('/v1/discovery/lyrics') || path.endsWith('/audio/recover')) {
      __http.replyOnce(`${method} ${path}`, { status: NOT_FOUND_STATUS });
    }
    return fakeFetch(url, init);
  }) as typeof fetch;
  return httpPlaybackPort;
}

async function restoreHttpPrefetchFlag(): Promise<void> {
  servePlaybackOverHttp({ prefetchEnabled: true });
  await httpPlaybackPort.fetchAudioUrls(['t1']);
}

beforeEach(() => {
  __http.reset();
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
});

afterEach(async () => {
  await restoreHttpPrefetchFlag();
  global.fetch = fakeFetch;
});

describe.each<[string, (world: PlaybackWorld) => PlaybackPort, boolean]>([
  [
    'createMemoryPlaybackPort',
    (world) =>
      createMemoryPlaybackPort({
        audioUrls: { t1: T1_AUDIO },
        lyrics: { Known: LYRICS },
        ...world,
      }),
    false,
  ],
  ['httpPlaybackPort over HTTP', servePlaybackOverHttp, true],
])('%s satisfies the PlaybackPort contract', (_name, portFor, overHttp) => {
  it('serves no urls and sends no request for an empty list', async () => {
    const port = portFor({});

    await expect(port.fetchAudioUrls([])).resolves.toEqual([]);

    if (overHttp) expect(__http.requests).toHaveLength(0);
  });

  it('serves the url and version of a requested track', async () => {
    const port = portFor({});

    await expect(port.fetchAudioUrls(['t1'])).resolves.toEqual([{ trackId: 't1', ...T1_AUDIO }]);
  });

  it('reports prefetch enabled until a fetch reports it disabled', async () => {
    const port = portFor({ prefetchEnabled: false });
    expect(port.isAudioPrefetchEnabled()).toBe(true);

    await port.fetchAudioUrls(['t1']);

    expect(port.isAudioPrefetchEnabled()).toBe(false);
  });

  it('serves back the saved queue state and replaces it on a second save', async () => {
    const port = portFor({});
    const a = queueState(['t1']);
    const b = queueState(['t2', 't3']);

    await port.saveQueueState(a);
    await expect(port.getQueueState()).resolves.toEqual(a);

    await port.saveQueueState(b);
    await expect(port.getQueueState()).resolves.toEqual(b);
  });

  it('rejects lyrics for an unknown title', async () => {
    const port = portFor({});

    await expect(port.getLyrics({ title: 'Unknown' })).rejects.toBeDefined();
  });

  it('rejects recovery of an unknown track', async () => {
    const port = portFor({});

    await expect(port.recoverAudio(asTrackId('unknown'))).rejects.toBeDefined();
  });

  it('builds the audio stream url from the track id', () => {
    const port = portFor({});

    expect(port.audioStreamUrl(T1)).toMatch(/\/v1\/tracks\/t1\/audio$/);
  });
});

describe('httpPlaybackPort wire', () => {
  it('refuses an http audio url with a ContractError', async () => {
    global.fetch = fakeFetch;
    __http.replyOnce(`POST ${AUDIO_URLS_PATH}`, {
      json: { urls: [{ track_id: 't1', url: 'http://cdn.test/t1.mp3', version: 'v1' }] },
    });

    await expect(httpPlaybackPort.fetchAudioUrls(['t1'])).rejects.toBeInstanceOf(ContractError);
  });

  it('sends no subtitle param for an empty subtitle', async () => {
    global.fetch = fakeFetch;
    __http.replyOnce('GET /v1/discovery/lyrics', { json: LYRICS });

    await httpPlaybackPort.getLyrics({ title: 'Known', subtitle: '' });

    expect(new URLSearchParams(__http.last().query).has('subtitle')).toBe(false);
    expect(__http.last().query).toContain('title=Known');
  });

  it('recovers audio with POST /v1/tracks/t1/audio/recover', async () => {
    global.fetch = fakeFetch;
    __http.replyOnce('POST /v1/tracks/t1/audio/recover', { status: NO_CONTENT_STATUS });

    await httpPlaybackPort.recoverAudio(T1);

    expect(__http.last()).toMatchObject({ method: 'POST', path: '/v1/tracks/t1/audio/recover' });
  });
});
