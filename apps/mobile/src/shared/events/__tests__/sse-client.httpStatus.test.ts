import {
  clearSessionExpired,
  getSessionExpired,
  renewSessionCredentials,
} from '@shared/auth/sessionExpired';

import { SSEClient, SSEHttpError } from '../sse-client';
import type { ServerEvent } from '../sse-client';

type Handler = () => void;

class StatusXHR {
  static instances: StatusXHR[] = [];

  responseText = '';
  status = 0;
  requestHeaders: Record<string, string> = {};
  responseHeaders: Record<string, string> = {};

  onprogress: Handler | null = null;
  onerror: Handler | null = null;
  onloadend: Handler | null = null;

  constructor() {
    StatusXHR.instances.push(this);
  }

  open(): void {}

  setRequestHeader(name: string, value: string): void {
    this.requestHeaders[name] = value;
  }

  getResponseHeader(name: string): string | null {
    return this.responseHeaders[name] ?? null;
  }

  send(): void {}

  abort(): void {}

  respond(status: number, body: string, headers: Record<string, string> = {}): void {
    this.status = status;
    this.responseHeaders = headers;
    this.responseText += body;
    this.onprogress?.();
    this.onloadend?.();
  }

  stream(chunk: string): void {
    this.status = 200;
    this.responseText += chunk;
    this.onprogress?.();
  }
}

const ERROR_BODY = '{"error":"rate_limited","message":"slow down"}\n\n';

function latest(): StatusXHR {
  const xhr = StatusXHR.instances[StatusXHR.instances.length - 1];
  if (!xhr) throw new Error('no StatusXHR opened');
  return xhr;
}

function makeClient() {
  const onEvent = jest.fn<void, [ServerEvent]>();
  const onError = jest.fn<void, [unknown]>();
  const client = new SSEClient(
    'https://api.example.com/v1/events',
    jest.fn<Promise<string | null>, []>().mockResolvedValue('token-1'),
    onEvent,
    onError,
  );
  return { client, onEvent, onError };
}

async function msUntilNextOpen(): Promise<number> {
  const before = StatusXHR.instances.length;
  let waited = 0;
  while (StatusXHR.instances.length === before) {
    await jest.advanceTimersByTimeAsync(100);
    waited += 100;
    if (waited > 10 * 60_000) throw new Error('client never reconnected');
  }
  return waited;
}

describe('SSEClient HTTP status handling', () => {
  beforeEach(() => {
    StatusXHR.instances = [];
    (global as unknown as { XMLHttpRequest: unknown }).XMLHttpRequest = StatusXHR;
    jest.useFakeTimers();
    jest.spyOn(Math, 'random').mockReturnValue(0);
    clearSessionExpired();
  });

  afterEach(() => {
    jest.useRealTimers();
    jest.restoreAllMocks();
    clearSessionExpired();
  });

  it.each([429, 502])(
    'grows the backoff to the cap when every %i response carries a body',
    async (status) => {
      const { client } = makeClient();
      await client.connect();

      const delays: number[] = [];
      for (let attempt = 0; attempt < 7; attempt++) {
        latest().respond(status, ERROR_BODY);
        delays.push(await msUntilNextOpen());
      }

      expect(delays).toEqual([1_000, 2_000, 4_000, 8_000, 16_000, 30_000, 30_000]);
    },
  );

  it('reports a refused stream through onError with its status and correlation id', async () => {
    const { client, onError } = makeClient();
    await client.connect();
    const sent = latest().requestHeaders['X-Correlation-ID'] ?? null;

    latest().respond(502, ERROR_BODY);

    expect(onError).toHaveBeenCalledTimes(1);
    const error = onError.mock.calls[0]?.[0];
    expect(error).toBeInstanceOf(SSEHttpError);
    expect(error).toMatchObject({ status: 502, correlationId: sent });
  });

  it('does not feed an error body to the event parser', async () => {
    const { client, onEvent, onError } = makeClient();
    await client.connect();

    latest().respond(500, 'id: 9\ndata: {"leak":true}\n\n');

    expect(onEvent).not.toHaveBeenCalled();
    expect(onError.mock.calls.map(([error]) => error)).toEqual([expect.any(SSEHttpError)]);
  });

  it('marks the session expired on a 401', async () => {
    const { client } = makeClient();
    await client.connect();

    latest().respond(401, '{"error":"unauthorized"}');

    expect(getSessionExpired()).toBe(true);
  });

  it('leaves the session alone on a refusal that is not a 401', async () => {
    const { client } = makeClient();
    await client.connect();

    latest().respond(403, '{"error":"forbidden"}');

    expect(getSessionExpired()).toBe(false);
  });

  it('does not expire credentials renewed after the refused stream was opened', async () => {
    const { client } = makeClient();
    await client.connect();

    renewSessionCredentials();
    latest().respond(401, '{"error":"unauthorized"}');

    expect(getSessionExpired()).toBe(false);
  });

  it('waits at least the Retry-After seconds before reconnecting', async () => {
    const { client } = makeClient();
    await client.connect();

    latest().respond(429, ERROR_BODY, { 'Retry-After': '12' });

    expect(await msUntilNextOpen()).toBe(12_000);
  });

  it('honours a Retry-After given as an HTTP date', async () => {
    const { client } = makeClient();
    await client.connect();

    const at = new Date(Date.now() + 7_000).toUTCString();
    latest().respond(503, ERROR_BODY, { 'Retry-After': at });

    const waited = await msUntilNextOpen();
    expect(waited).toBeGreaterThanOrEqual(6_000);
    expect(waited).toBeLessThanOrEqual(7_000);
  });

  it('bounds a hostile Retry-After to five minutes', async () => {
    const { client } = makeClient();
    await client.connect();

    latest().respond(429, ERROR_BODY, { 'Retry-After': '999999999' });

    expect(await msUntilNextOpen()).toBe(5 * 60_000);
  });

  it('falls back to the backoff when Retry-After is unparseable', async () => {
    const { client } = makeClient();
    await client.connect();

    latest().respond(429, ERROR_BODY, { 'Retry-After': 'soon' });

    expect(await msUntilNextOpen()).toBe(1_000);
  });

  it('resets the backoff once a 200 stream delivers bytes after refusals', async () => {
    const { client, onEvent } = makeClient();
    await client.connect();

    latest().respond(502, ERROR_BODY);
    await msUntilNextOpen();
    latest().respond(502, ERROR_BODY);
    await msUntilNextOpen();

    latest().stream('id: 1\ndata: {"a":1}\n\n');
    latest().onloadend?.();

    expect(onEvent).toHaveBeenCalledTimes(1);
    expect(await msUntilNextOpen()).toBe(1_000);
  });
});
