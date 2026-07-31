import { getQueueState, saveQueueState } from '../playback';
import type { SaveQueueStateRequest } from '../playback';
import { ApiError, NetworkError } from '../index';
import { supabase } from '@shared/auth/supabaseClient';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const getSession = supabase.auth.getSession as jest.Mock;

function withSession(accessToken = 'tok') {
  getSession.mockResolvedValue({
    data: { session: { access_token: accessToken } },
    error: null,
  });
}

beforeEach(() => {
  getSession.mockReset();
  withSession();
});

function fullQueueStateWire() {
  return {
    track_ids: ['t1', 't2'],
    current_index: 0,
    position_ms: 1000,
    shuffled: false,
    repeat_mode: 'off',
    source: { kind: 'library' },
    natural_order: ['t1', 't2'],
    current_track: {
      id: 't1',
      title: 'Track One',
      artist: 'Artist One',
      artwork_url: null,
      duration_seconds: 180,
      acquisition_status: 'ready',
    },
  };
}

describe('getQueueState', () => {
  it('GETs /v1/playback/queue-state', async () => {
    __http.reply('GET /v1/playback/queue-state', { status: 200, json: fullQueueStateWire() });

    await getQueueState();

    expect(__http.last().method).toBe('GET');
    expect(__http.last().path).toBe('/v1/playback/queue-state');
  });

  it('resolves the full queue state a resumed session needs', async () => {
    const wire = fullQueueStateWire();
    __http.reply('GET /v1/playback/queue-state', { status: 200, json: wire });

    await expect(getQueueState()).resolves.toEqual(wire);
  });

  it('rejects with ApiError when the server has no saved queue state', async () => {
    __http.reply('GET /v1/playback/queue-state', { status: 404, json: {} });

    await expect(getQueueState()).rejects.toMatchObject({ status: 404 });
  });

  it('rejects with NetworkError on a dropped connection', async () => {
    __http.fail('GET /v1/playback/queue-state');

    await expect(getQueueState()).rejects.toBeInstanceOf(NetworkError);
  });

  it('rejects with NetworkError on a truncated body', async () => {
    __http.reply('GET /v1/playback/queue-state', { status: 200, malformed: true });

    await expect(getQueueState()).rejects.toBeInstanceOf(NetworkError);
  });

  describe('legacy client shapes', () => {
    it('loads a row from a pre-natural-order client where natural_order is absent', async () => {
      const wire = {
        track_ids: ['t1'],
        current_index: 0,
        position_ms: 0,
        shuffled: false,
        repeat_mode: 'off',
        source: null,
      };
      __http.reply('GET /v1/playback/queue-state', { status: 200, json: wire });

      const result = await getQueueState();
      expect(result.natural_order).toBeUndefined();
      expect(result.track_ids).toEqual(['t1']);
    });

    it('loads a row where natural_order is an empty array', async () => {
      const wire = { ...fullQueueStateWire(), natural_order: [] };
      __http.reply('GET /v1/playback/queue-state', { status: 200, json: wire });

      await expect(getQueueState()).resolves.toMatchObject({ natural_order: [] });
    });

    it('loads a row with no current_track (nothing was playing on last save)', async () => {
      const wire: Record<string, unknown> = { ...fullQueueStateWire() };
      delete wire.current_track;
      __http.reply('GET /v1/playback/queue-state', { status: 200, json: wire });

      const result = await getQueueState();
      expect(result.current_track).toBeUndefined();
    });

    it('loads a row where source is null', async () => {
      const wire = { ...fullQueueStateWire(), source: null };
      __http.reply('GET /v1/playback/queue-state', { status: 200, json: wire });

      await expect(getQueueState()).resolves.toMatchObject({ source: null });
    });

    it('loads a row that still carries the legacy source_id field alongside a null source', async () => {
      const wire = { ...fullQueueStateWire(), source: null, source_id: 'playlist:p1' };
      __http.reply('GET /v1/playback/queue-state', { status: 200, json: wire });

      const result = (await getQueueState()) as typeof wire;
      expect(result.source).toBeNull();
      expect(result.source_id).toBe('playlist:p1');
    });
  });

  it('never leaks the access token into the queue-state request URL', async () => {
    withSession('leak-check-token');
    __http.reply('GET /v1/playback/queue-state', { status: 200, json: fullQueueStateWire() });

    await getQueueState();

    expect(__http.last().url).not.toContain('leak-check-token');
  });
});

describe('saveQueueState', () => {
  const body: SaveQueueStateRequest = {
    track_ids: ['t1', 't2'],
    current_index: 1,
    position_ms: 4200,
    shuffled: false,
    repeat_mode: 'off',
    source: { kind: 'library' },
    natural_order: ['t1', 't2'],
  };

  it('PUTs the exact serialized body with a JSON Content-Type', async () => {
    __http.reply('PUT /v1/playback/queue-state', { status: 204 });

    await saveQueueState(body);

    const request = __http.last();
    expect(request.method).toBe('PUT');
    expect(request.path).toBe('/v1/playback/queue-state');
    expect(request.headers['Content-Type']).toBe('application/json');
    expect(JSON.parse(request.body)).toEqual(body);
  });

  it('resolves on the 204 this endpoint returns on success', async () => {
    __http.reply('PUT /v1/playback/queue-state', { status: 204 });

    await expect(saveQueueState(body)).resolves.toBeUndefined();
  });

  it("rejects with ApiError on a server rejection, for the caller's try/catch {} to swallow", async () => {
    __http.reply('PUT /v1/playback/queue-state', { status: 500, json: {} });

    await expect(saveQueueState(body)).rejects.toBeInstanceOf(ApiError);
  });

  it('rejects with NetworkError when the network drops mid-save', async () => {
    __http.fail('PUT /v1/playback/queue-state');

    await expect(saveQueueState(body)).rejects.toBeInstanceOf(NetworkError);
  });

  it('never leaks the access token into the save request URL', async () => {
    withSession('leak-check-token');
    __http.reply('PUT /v1/playback/queue-state', { status: 204 });

    await saveQueueState(body);

    expect(__http.last().url).not.toContain('leak-check-token');
  });
});
