import {
  backfillFeaturedArtists,
  createTrack,
  deleteTrack,
  getAllTracks,
  getTracks,
  MAX_ALL_TRACKS,
  listTracksFeaturing,
  makeIdempotencyKey,
  reacquireTrack,
  retryAcquisition,
} from '../tracks';
import { ContractError, NetworkError } from '@shared/errors';
import { supabase } from '@shared/auth/supabaseClient';
import { asTrackId, type TrackId } from '@shared/api-client/ids';
import type { CreateTrackRequest, FeaturedArtist, TrackResponse } from '../types';

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

afterEach(() => {
  jest.restoreAllMocks();
});

function trackResponse(overrides: Partial<TrackResponse> = {}): TrackResponse {
  return {
    id: asTrackId('t1'),
    title: 'Kid A',
    artist: 'Radiohead',
    album: null,
    duration_seconds: null,
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

function baseCreateBody(): CreateTrackRequest {
  return {
    title: 'Kid A',
    artist: 'Radiohead',
    album: null,
    duration_seconds: null,
    artwork_url: null,
    isrc: null,
    year: null,
    genre: null,
    album_artist: null,
    track_number: null,
  };
}

describe('getTracks', () => {
  it('sends q and sort together when the search box and a sort chip are both set', async () => {
    __http.reply('GET /v1/tracks', {
      status: 200,
      json: { items: [], total: 0, limit: 20, offset: 0, has_more: false },
    });

    await getTracks({ limit: 20, offset: 0, q: 'kid a', sort: 'az' });

    const params = new URLSearchParams(__http.last().query);
    expect(params.get('q')).toBe('kid a');
    expect(params.get('sort')).toBe('az');
    expect(params.get('limit')).toBe('20');
    expect(params.get('offset')).toBe('0');
  });

  it('sends only limit and offset when rehydrating a queue with neither q nor sort set', async () => {
    __http.reply('GET /v1/tracks', {
      status: 200,
      json: { items: [], total: 0, limit: 50, offset: 0, has_more: false },
    });

    await getTracks({ limit: 50, offset: 0 });

    const params = new URLSearchParams(__http.last().query);
    expect(params.has('q')).toBe(false);
    expect(params.has('sort')).toBe(false);
    expect(params.get('limit')).toBe('50');
    expect(params.get('offset')).toBe('0');
  });

  it('sends q alone for a title lookup with no sort chip active', async () => {
    __http.reply('GET /v1/tracks', {
      status: 200,
      json: { items: [], total: 0, limit: 200, offset: 0, has_more: false },
    });

    await getTracks({ limit: 200, offset: 0, q: 'Kid A' });

    const params = new URLSearchParams(__http.last().query);
    expect(params.get('q')).toBe('Kid A');
    expect(params.has('sort')).toBe(false);
  });

  it('omits q when it is the empty string, rather than sending an empty search term', async () => {
    __http.reply('GET /v1/tracks', {
      status: 200,
      json: { items: [], total: 0, limit: 20, offset: 0, has_more: false },
    });

    await getTracks({ limit: 20, offset: 0, q: '' });

    const params = new URLSearchParams(__http.last().query);
    expect(params.has('q')).toBe(false);
  });

  it('stringifies a zero limit and offset rather than omitting them', async () => {
    __http.reply('GET /v1/tracks', {
      status: 200,
      json: { items: [], total: 0, limit: 0, offset: 0, has_more: false },
    });

    await getTracks({ limit: 0, offset: 0 });

    const params = new URLSearchParams(__http.last().query);
    expect(params.get('limit')).toBe('0');
    expect(params.get('offset')).toBe('0');
  });

  it('rejects a response missing its required items array as a ContractError, not a silent partial', async () => {
    __http.reply('GET /v1/tracks', {
      status: 200,
      json: { total: 0, limit: 20, offset: 0, has_more: false },
    });

    await expect(getTracks({ limit: 20, offset: 0 })).rejects.toBeInstanceOf(ContractError);
  });

  it('rejects a null body as a ContractError rather than handing null to the caller', async () => {
    __http.reply('GET /v1/tracks', { status: 200, json: null });

    await expect(getTracks({ limit: 20, offset: 0 })).rejects.toBeInstanceOf(ContractError);
  });

  it('surfaces a transport failure as NetworkError rather than swallowing it', async () => {
    __http.fail('GET /v1/tracks');

    await expect(getTracks({ limit: 20, offset: 0 })).rejects.toBeInstanceOf(NetworkError);
  });

  it('forwards a caller abort signal so a superseded search stops its in-flight request (#794)', async () => {
    __http.hang('GET /v1/tracks');
    const controller = new AbortController();

    const pending = getTracks({ limit: 20, offset: 0, q: 'kid' }, controller.signal);
    while (__http.requests.length === 0) await new Promise((r) => setTimeout(r, 0));
    controller.abort();

    await expect(pending).rejects.toThrow();
    expect(__http.last().signal.aborted).toBe(true);
  });
});

describe('createTrack', () => {
  it('POSTs the track with JSON content-type and no optional fields when neither is supplied', async () => {
    __http.reply('POST /v1/tracks', { status: 201, json: trackResponse() });

    await createTrack(baseCreateBody());

    const request = __http.last();
    expect(request.method).toBe('POST');
    expect(request.path).toBe('/v1/tracks');
    expect(request.headers['Content-Type']).toBe('application/json');
    const sent = JSON.parse(request.body);
    expect(sent.featured_artists).toBeUndefined();
    expect(sent.source_url).toBeUndefined();
    expect(sent.title).toBe('Kid A');
    expect(sent.artist).toBe('Radiohead');
  });

  it('mints and sends an Idempotency-Key header so the server can collapse retries', async () => {
    __http.reply('POST /v1/tracks', { status: 201, json: trackResponse() });

    await createTrack(baseCreateBody());

    const key = __http.last().headers['Idempotency-Key'];
    expect(key).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
  });

  it('mints a key no observer of Math.random can predict (#1774)', () => {
    jest.spyOn(Math, 'random').mockReturnValue(0.5);

    const keys = [makeIdempotencyKey(), makeIdempotencyKey()];

    expect(keys[0]).not.toBe(keys[1]);
    expect(keys[0]).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
  });

  it('forwards a caller-supplied idempotency key unchanged', async () => {
    __http.reply('POST /v1/tracks', { status: 201, json: trackResponse() });

    await createTrack(baseCreateBody(), 'stable-save-key-1');

    expect(__http.last().headers['Idempotency-Key']).toBe('stable-save-key-1');
  });

  it('serializes featured_artists and source_url when the caller supplies them', async () => {
    __http.reply('POST /v1/tracks', { status: 201, json: trackResponse() });
    const featured: FeaturedArtist[] = [{ name: 'Thom Yorke', mbid: 'mbid-1', deezer_id: 0 }];

    await createTrack({
      ...baseCreateBody(),
      featured_artists: featured,
      source_url: 'https://example.com/track',
    });

    const sent = JSON.parse(__http.last().body);
    expect(sent.featured_artists).toEqual(featured);
    expect(sent.source_url).toBe('https://example.com/track');
  });

  it('resolves with the server track on success', async () => {
    __http.reply('POST /v1/tracks', {
      status: 201,
      json: trackResponse({ id: asTrackId('t1'), title: 'Kid A', acquisition_status: 'pending' }),
    });

    await expect(createTrack(baseCreateBody())).resolves.toMatchObject({
      id: 't1',
      acquisition_status: 'pending',
    });
  });
});

describe('deleteTrack', () => {
  it('DELETEs the exact track path and resolves void on 204', async () => {
    __http.reply('DELETE /v1/tracks/t1', { status: 204 });

    await expect(deleteTrack(asTrackId('t1'))).resolves.toBeUndefined();
    expect(__http.last().method).toBe('DELETE');
    expect(__http.last().path).toBe('/v1/tracks/t1');
  });

});

describe('track id path safety (#944)', () => {
  // Before #944 every track endpoint here spliced the id into its path raw, so an id of
  // `t1/track-number` DELETEd a different route. A smuggled (cast) id must be refused unsent.
  const endpoints = [
    ['deleteTrack', (id: TrackId) => deleteTrack(id)],
    ['retryAcquisition', (id: TrackId) => retryAcquisition(id)],
    ['reacquireTrack', (id: TrackId) => reacquireTrack(id)],
  ] as const;

  describe.each(endpoints)('%s', (_name, call) => {
    it.each(['t1/track-number', '..', '%2e%2e', 't1?x=1', 't1#frag', ''])(
      'refuses the id %p without sending a request',
      async (id) => {
        __http.replyAll({ status: 204 });

        await expect(call(id as TrackId)).rejects.toBeInstanceOf(ContractError);

        expect(__http.requests).toHaveLength(0);
      },
    );
  });
});

describe('retryAcquisition and reacquireTrack are distinct endpoints', () => {
  it('retryAcquisition POSTs to /retry', async () => {
    __http.reply('POST /v1/tracks/t1/retry', { status: 204 });

    await retryAcquisition(asTrackId('t1'));

    expect(__http.last().method).toBe('POST');
    expect(__http.last().path).toBe('/v1/tracks/t1/retry');
  });

  it('reacquireTrack POSTs to /reacquire, a different path from retry', async () => {
    __http.reply('POST /v1/tracks/t1/reacquire', { status: 204 });

    await reacquireTrack(asTrackId('t1'));

    expect(__http.last().method).toBe('POST');
    expect(__http.last().path).toBe('/v1/tracks/t1/reacquire');
  });

  it('retryAcquisition never touches the reacquire path', async () => {
    __http.reply('POST /v1/tracks/t1/retry', { status: 204 });

    await retryAcquisition(asTrackId('t1'));

    expect(__http.countFor('POST /v1/tracks/t1/reacquire')).toBe(0);
  });
});

describe('listTracksFeaturing', () => {
  it('sends mbid alone when only mbid is known', async () => {
    __http.reply('GET /v1/tracks/featuring', {
      status: 200,
      json: { items: [], total: 0, limit: 0, offset: 0, has_more: false },
    });

    await listTracksFeaturing({ name: '', mbid: 'mbid-1', deezer_id: null });

    const params = new URLSearchParams(__http.last().query);
    expect(params.get('mbid')).toBe('mbid-1');
    expect(params.has('deezer_id')).toBe(false);
    expect(params.has('name')).toBe(false);
  });

  it('sends deezer_id 0 because 0 is a valid id and the guard checks != null, not truthiness', async () => {
    __http.reply('GET /v1/tracks/featuring', {
      status: 200,
      json: { items: [], total: 0, limit: 0, offset: 0, has_more: false },
    });

    await listTracksFeaturing({ name: '', mbid: null, deezer_id: 0 });

    const params = new URLSearchParams(__http.last().query);
    expect(params.get('deezer_id')).toBe('0');
  });

  it('sends name alone when neither identifier is known', async () => {
    __http.reply('GET /v1/tracks/featuring', {
      status: 200,
      json: { items: [], total: 0, limit: 0, offset: 0, has_more: false },
    });

    await listTracksFeaturing({ name: 'Thom Yorke', mbid: null, deezer_id: null });

    const params = new URLSearchParams(__http.last().query);
    expect(params.get('name')).toBe('Thom Yorke');
    expect(params.has('mbid')).toBe(false);
    expect(params.has('deezer_id')).toBe(false);
  });

  it('omits name when it is the empty string, distinct from a real deezer_id of 0', async () => {
    __http.reply('GET /v1/tracks/featuring', {
      status: 200,
      json: { items: [], total: 0, limit: 0, offset: 0, has_more: false },
    });

    await listTracksFeaturing({ name: '', mbid: null, deezer_id: null });

    expect(__http.last().query).toBe('');
  });

  it('sends all three identity params together when all are known', async () => {
    __http.reply('GET /v1/tracks/featuring', {
      status: 200,
      json: { items: [], total: 0, limit: 0, offset: 0, has_more: false },
    });

    await listTracksFeaturing({ name: 'Thom Yorke', mbid: 'mbid-1', deezer_id: 42 });

    const params = new URLSearchParams(__http.last().query);
    expect(params.get('mbid')).toBe('mbid-1');
    expect(params.get('deezer_id')).toBe('42');
    expect(params.get('name')).toBe('Thom Yorke');
  });
});

describe('backfillFeaturedArtists', () => {
  it('POSTs the backfill endpoint and resolves the scanned/updated counts', async () => {
    __http.reply('POST /v1/tracks/featured-backfill', {
      status: 200,
      json: { scanned: 120, updated: 8 },
    });

    await expect(backfillFeaturedArtists()).resolves.toEqual({ scanned: 120, updated: 8 });
    expect(__http.last().method).toBe('POST');
    expect(__http.last().path).toBe('/v1/tracks/featured-backfill');
  });

  // #843: the counts are interpolated into settings copy, so an off-contract body
  // must fail as a ContractError rather than resolve as garbage.
  it.each([
    ['updated exceeds scanned', { scanned: 3, updated: 12 }],
    ['missing fields', {}],
    ['null fields', { scanned: null, updated: null }],
    ['non-numeric fields', { scanned: '40', updated: '3' }],
    ['fractional count', { scanned: 4.5, updated: 1 }],
    ['negative count', { scanned: 5, updated: -1 }],
    ['non-object body', [1, 2]],
  ])('rejects an off-contract body (%s) with a ContractError', async (_label, json) => {
    __http.reply('POST /v1/tracks/featured-backfill', { status: 200, json });

    await expect(backfillFeaturedArtists()).rejects.toBeInstanceOf(ContractError);
  });

  it('accepts a zero-count run', async () => {
    __http.reply('POST /v1/tracks/featured-backfill', {
      status: 200,
      json: { scanned: 0, updated: 0 },
    });

    await expect(backfillFeaturedArtists()).resolves.toEqual({ scanned: 0, updated: 0 });
  });
});

describe('getAllTracks', () => {
  function page(items: { id: string }[], offset: number, total: number, hasMore: boolean) {
    return {
      status: 200,
      json: {
        items: items.map((it) => trackResponse({ id: asTrackId(it.id) })),
        total,
        limit: 2000,
        offset,
        has_more: hasMore,
      },
    };
  }

  it('returns the single page when the server says there is no more', async () => {
    __http.reply('GET /v1/tracks', page([{ id: 'a' }, { id: 'b' }], 0, 2, false));

    await expect(getAllTracks({})).resolves.toHaveLength(2);
    expect(__http.countFor('GET /v1/tracks')).toBe(1);
  });

  it('follows has_more and offsets each request by what it has already collected', async () => {
    __http.replyOnce('GET /v1/tracks', page([{ id: 'a' }, { id: 'b' }], 0, 3, true));
    __http.replyOnce('GET /v1/tracks', page([{ id: 'c' }], 2, 3, false));

    const all = await getAllTracks({});

    expect(all.map((t) => t.id)).toEqual(['a', 'b', 'c']);
    expect(__http.countFor('GET /v1/tracks')).toBe(2);
    expect(new URLSearchParams(__http.requests[1].query).get('offset')).toBe('2');
  });

  it('requests the server cap as the page size, so a whole library is at most a few calls', async () => {
    __http.reply('GET /v1/tracks', page([], 0, 0, false));

    await getAllTracks({});

    expect(new URLSearchParams(__http.last().query).get('limit')).toBe('2000');
  });

  it('stops on an empty page even when the server still claims has_more, rather than looping forever', async () => {
    __http.reply('GET /v1/tracks', page([], 0, 99, true));

    await expect(getAllTracks({})).resolves.toEqual([]);
    expect(__http.countFor('GET /v1/tracks')).toBe(1);
  });

  it('stops at MAX_ALL_TRACKS and warns, rather than paging an endless has_more forever (#790)', async () => {
    const fullPage = Array.from({ length: 2000 }, (_, i) => ({ id: `t${i}` }));
    __http.reply('GET /v1/tracks', page(fullPage, 0, 1_000_000, true));
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => {});

    const all = await getAllTracks({});

    expect(all).toHaveLength(MAX_ALL_TRACKS);
    expect(__http.countFor('GET /v1/tracks')).toBe(MAX_ALL_TRACKS / 2000);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('[library]'), expect.anything());
    warn.mockRestore();
  });

  it('forwards q and sort to every page, so a filtered collection pages under the same filter', async () => {
    __http.replyOnce('GET /v1/tracks', page([{ id: 'a' }], 0, 2, true));
    __http.replyOnce('GET /v1/tracks', page([{ id: 'b' }], 1, 2, false));

    await getAllTracks({ q: 'radiohead', sort: 'az' });

    for (const request of __http.requests) {
      const params = new URLSearchParams(request.query);
      expect(params.get('q')).toBe('radiohead');
      expect(params.get('sort')).toBe('az');
    }
  });
});
