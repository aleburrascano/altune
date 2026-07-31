import { ApiError, NetworkError } from '@shared/api-client';
import { supabase } from '@shared/auth/supabaseClient';

import { recordEvent, type DiscoveryEvent } from '../recordEvent';
import { getSessionId } from '../session';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

jest.mock('../session', () => ({ getSessionId: jest.fn() }));

const getSession = supabase.auth.getSession as jest.Mock;
const sessionId = getSessionId as jest.Mock;

function withSession(accessToken: string | null = 'tok') {
  getSession.mockResolvedValue({
    data: { session: accessToken == null ? null : { access_token: accessToken } },
    error: null,
  });
}

function sentBody(): DiscoveryEvent {
  return JSON.parse(__http.last().body);
}

beforeEach(() => {
  getSession.mockReset();
  sessionId.mockReset();
  withSession();
  sessionId.mockReturnValue('stamped-session-1');
});

afterEach(() => {
  jest.useRealTimers();
});

describe('recordEvent(): payload merge with the stamped session_id', () => {
  it('stamps session_id alone when payload is absent', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });

    await recordEvent({ type: 'play' });

    expect(sentBody().payload).toEqual({ session_id: 'stamped-session-1' });
  });

  it('stamps session_id alone when payload is explicitly undefined', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });

    const event = { type: 'play', payload: undefined } as unknown as DiscoveryEvent;

    await recordEvent(event);

    expect(sentBody().payload).toEqual({ session_id: 'stamped-session-1' });
  });

  it('stamps session_id alone when payload is null', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });
    const event = { type: 'play', payload: null } as unknown as DiscoveryEvent;

    await recordEvent(event);

    expect(sentBody().payload).toEqual({ session_id: 'stamped-session-1' });
  });

  it('stamps session_id alone when payload is an empty object', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });

    await recordEvent({ type: 'play', payload: {} });

    expect(sentBody().payload).toEqual({ session_id: 'stamped-session-1' });
  });

  it('adds session_id to a populated payload without dropping the caller keys', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });

    await recordEvent({ type: 'play', payload: { video_id: 'abc', dwell_ms: 1500 } });

    expect(sentBody().payload).toEqual({
      video_id: 'abc',
      dwell_ms: 1500,
      session_id: 'stamped-session-1',
    });
  });

  it('the stamped session_id wins over a caller-supplied session_id in payload', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });

    await recordEvent({ type: 'play', payload: { session_id: 'caller-supplied' } });

    expect(sentBody().payload).toEqual({ session_id: 'stamped-session-1' });
  });
});

describe('recordEvent(): public interface — InteractionEvent submission', () => {
  it('POSTs to /v1/discovery/events with JSON content-type and the authenticated bearer token', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });

    await recordEvent({ type: 'skip' });

    const request = __http.last();
    expect(request.method).toBe('POST');
    expect(request.path).toBe('/v1/discovery/events');
    expect(request.headers['Content-Type']).toBe('application/json');
    expect(request.headers.Authorization).toBe('Bearer tok');
  });

  it('passes type, query_norm, search_id, event_id and client_occurred_at through unchanged', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });

    await recordEvent({
      type: 'result_clicked',
      query_norm: 'daft punk',
      search_id: 'search-1',
      event_id: 'evt-1',
      client_occurred_at: '2026-07-31T00:00:00.000Z',
    });

    expect(sentBody()).toMatchObject({
      type: 'result_clicked',
      query_norm: 'daft punk',
      search_id: 'search-1',
      event_id: 'evt-1',
      client_occurred_at: '2026-07-31T00:00:00.000Z',
    });
  });

  it('resolves with no value on success, as its Promise<void> contract promises', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });

    await expect(recordEvent({ type: 'completed' })).resolves.toBeUndefined();
  });
});

describe('recordEvent(): adversarial server responses', () => {
  it('rejects with ApiError(400) on a rejected/malformed event', async () => {
    __http.reply('POST /v1/discovery/events', { status: 400, json: { message: 'bad request' } });

    await expect(recordEvent({ type: 'play' })).rejects.toBeInstanceOf(ApiError);
    await expect(recordEvent({ type: 'play' })).rejects.toMatchObject({ status: 400 });
  });

  it('rejects with ApiError(401) on a server-side session rejection (expired/replayed token)', async () => {
    __http.reply('POST /v1/discovery/events', { status: 401, json: { message: 'revoked' } });

    await expect(recordEvent({ type: 'play' })).rejects.toMatchObject({ status: 401 });
  });

  it('rejects with ApiError(500) on a backend fault', async () => {
    __http.reply('POST /v1/discovery/events', { status: 500, json: { message: 'boom' } });

    await expect(recordEvent({ type: 'play' })).rejects.toMatchObject({ status: 500 });
  });

  it('rejects with NetworkError(transport) on a malformed/truncated 200 body', async () => {
    __http.reply('POST /v1/discovery/events', { status: 200, malformed: true });

    await expect(recordEvent({ type: 'play' })).rejects.toBeInstanceOf(NetworkError);
    await expect(recordEvent({ type: 'play' })).rejects.toMatchObject({ failure: 'transport' });
  });

  it('rejects with NetworkError(timeout) when the request hangs past the 15s deadline', async () => {
    jest.useFakeTimers();
    __http.hang('POST /v1/discovery/events');

    const pending = recordEvent({ type: 'play' });
    const caught = pending.catch((e: Error) => e);
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();

    jest.advanceTimersByTime(15_000);

    await expect(caught).resolves.toMatchObject({ name: 'NetworkError', failure: 'timeout' });
  });

  it('rejects with NetworkError(transport) on a raw transport failure', async () => {
    __http.fail('POST /v1/discovery/events', __http.transportError());

    await expect(recordEvent({ type: 'play' })).rejects.toBeInstanceOf(NetworkError);
    await expect(recordEvent({ type: 'play' })).rejects.toMatchObject({ failure: 'transport' });
  });
});

describe('recordEvent(): failure injection — the one I/O call site, apiFetch', () => {
  it('rejects with ApiError(401) and never reaches the wire when there is no active session', async () => {
    withSession(null);

    await expect(recordEvent({ type: 'play' })).rejects.toBeInstanceOf(ApiError);
    expect(__http.requests.length).toBe(0);
  });
});
