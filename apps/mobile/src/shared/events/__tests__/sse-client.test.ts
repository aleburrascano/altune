import { SSEClient, HEARTBEAT_WATCHDOG_MS, MAX_RESPONSE_BYTES } from '../sse-client';
import type { ServerEvent } from '../sse-client';

type Handler = () => void;

class FakeXHR {
  static instances: FakeXHR[] = [];

  responseText = '';
  method = '';
  url = '';
  requestHeaders: Record<string, string> = {};
  aborted = false;
  sent = false;

  onprogress: Handler | null = null;
  onerror: Handler | null = null;
  onloadend: Handler | null = null;

  constructor() {
    FakeXHR.instances.push(this);
  }

  open(method: string, url: string): void {
    this.method = method;
    this.url = url;
  }

  setRequestHeader(name: string, value: string): void {
    this.requestHeaders[name] = value;
  }

  send(): void {
    this.sent = true;
  }

  abort(): void {
    this.aborted = true;
  }

  emit(chunk: string): void {
    this.responseText += chunk;
    this.onprogress?.();
  }

  triggerError(): void {
    this.onerror?.();
  }

  triggerLoadEnd(): void {
    this.onloadend?.();
  }
}

function xhrAt(index: number): FakeXHR {
  const xhr = FakeXHR.instances[index];
  if (!xhr) throw new Error(`no FakeXHR instance at index ${index}`);
  return xhr;
}

function block({
  id = '',
  type = '',
  data,
}: {
  id?: string;
  type?: string;
  data: unknown;
}): string {
  const lines: string[] = [];
  if (id) lines.push(`id: ${id}`);
  if (type) lines.push(`event: ${type}`);
  lines.push(`data: ${JSON.stringify(data)}`);
  return `${lines.join('\n')}\n\n`;
}

async function flush(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

function makeClient(getToken = jest.fn<Promise<string | null>, []>().mockResolvedValue('token-1')) {
  const onEvent = jest.fn<void, [ServerEvent]>();
  const onError = jest.fn<void, [unknown]>();
  const client = new SSEClient('https://api.example.com/v1/events', getToken, onEvent, onError);
  return { client, onEvent, onError, getToken };
}

describe('SSEClient', () => {
  beforeEach(() => {
    FakeXHR.instances = [];
    (global as unknown as { XMLHttpRequest: unknown }).XMLHttpRequest = FakeXHR;
    jest.useFakeTimers();
  });

  afterEach(() => {
    jest.useRealTimers();
    jest.restoreAllMocks();
  });

  describe('request contract', () => {
    it('opens a GET and sends the bearer token and SSE Accept header', async () => {
      const { client } = makeClient();
      await client.connect();

      const xhr = xhrAt(0);
      expect(xhr.method).toBe('GET');
      expect(xhr.url).toBe('https://api.example.com/v1/events');
      expect(xhr.requestHeaders.Authorization).toBe('Bearer token-1');
      expect(xhr.requestHeaders.Accept).toBe('text/event-stream');
      expect(xhr.sent).toBe(true);
    });
  });

  describe('framing', () => {
    it('dispatches a well-formed event with id, type and data', async () => {
      const { client, onEvent } = makeClient();
      await client.connect();

      xhrAt(0).emit(block({ id: '1', type: 'track_added_to_library', data: { a: 1 } }));

      expect(onEvent).toHaveBeenCalledTimes(1);
      expect(onEvent).toHaveBeenCalledWith({
        id: '1',
        type: 'track_added_to_library',
        data: { a: 1 },
      });
    });

    it('defaults the event type to message when no event line is present', async () => {
      const { client, onEvent } = makeClient();
      await client.connect();

      xhrAt(0).emit(`id: 1\ndata: {"a":1}\n\n`);

      expect(onEvent).toHaveBeenCalledWith({ id: '1', type: 'message', data: { a: 1 } });
    });

    it('defaults id to empty string when no id line is present', async () => {
      const { client, onEvent } = makeClient();
      await client.connect();

      xhrAt(0).emit(`event: resync\ndata: {}\n\n`);

      expect(onEvent).toHaveBeenCalledWith({ id: '', type: 'resync', data: {} });
    });

    it('dispatches multiple events found in a single chunk, in order', async () => {
      const { client, onEvent } = makeClient();
      await client.connect();

      xhrAt(0).emit(block({ id: '1', data: { seq: 1 } }) + block({ id: '2', data: { seq: 2 } }));

      expect(onEvent).toHaveBeenCalledTimes(2);
      expect(onEvent.mock.calls[0]?.[0]?.data).toEqual({ seq: 1 });
      expect(onEvent.mock.calls[1]?.[0]?.data).toEqual({ seq: 2 });
    });

    it('reassembles a block split across a chunk boundary mid-data-line', async () => {
      const { client, onEvent } = makeClient();
      await client.connect();
      const xhr = xhrAt(0);

      xhr.emit('id: 1\ndata: {"a":1');
      expect(onEvent).not.toHaveBeenCalled();

      xhr.emit('}\n\n');
      expect(onEvent).toHaveBeenCalledTimes(1);
      expect(onEvent).toHaveBeenCalledWith({ id: '1', type: 'message', data: { a: 1 } });
    });

    it('reassembles a block delivered byte-by-byte', async () => {
      const { client, onEvent } = makeClient();
      await client.connect();
      const xhr = xhrAt(0);
      const full = block({ id: '9', type: 'resync', data: { deep: { nested: true } } });

      for (const char of full) {
        xhr.emit(char);
      }

      expect(onEvent).toHaveBeenCalledTimes(1);
      expect(onEvent).toHaveBeenCalledWith({
        id: '9',
        type: 'resync',
        data: { deep: { nested: true } },
      });
    });
  });

  describe('adversarial frames', () => {
    it('drops a block with no data line, without dispatching or throwing', async () => {
      const { client, onEvent } = makeClient();
      await client.connect();

      expect(() => xhrAt(0).emit('id: 1\nevent: ping\n\n')).not.toThrow();
      expect(onEvent).not.toHaveBeenCalled();
    });

    it('dispatches an event of an unrecognized type unchanged', async () => {
      const { client, onEvent } = makeClient();
      await client.connect();

      xhrAt(0).emit(block({ id: '1', type: 'a_type_nobody_declared', data: { x: 1 } }));

      expect(onEvent).toHaveBeenCalledWith({
        id: '1',
        type: 'a_type_nobody_declared',
        data: { x: 1 },
      });
    });

    it('drops a block whose data is not valid JSON but still dispatches a valid block in the same chunk', async () => {
      const { client, onEvent } = makeClient();
      await client.connect();

      xhrAt(0).emit('id: 1\ndata: not-json\n\nid: 2\ndata: {"ok":true}\n\n');

      expect(onEvent).toHaveBeenCalledTimes(1);
      expect(onEvent).toHaveBeenCalledWith({ id: '2', type: 'message', data: { ok: true } });
    });

    it('ignores blank blocks (heartbeat padding) between real events', async () => {
      const { client, onEvent } = makeClient();
      await client.connect();

      xhrAt(0).emit(`\n\n${block({ id: '1', data: { a: 1 } })}`);

      expect(onEvent).toHaveBeenCalledTimes(1);
    });

    it('ignores unrecognized field lines such as SSE comments or retry:', async () => {
      const { client, onEvent } = makeClient();
      await client.connect();

      xhrAt(0).emit(': keepalive\nretry: 5000\nid: 1\ndata: {"a":1}\n\n');

      expect(onEvent).toHaveBeenCalledWith({ id: '1', type: 'message', data: { a: 1 } });
    });

    it('joins multiple data: lines with a newline, per the SSE spec', async () => {
      const { client, onEvent } = makeClient();
      await client.connect();

      xhrAt(0).emit('id: 1\ndata: {"split":\ndata: 2}\n\n');

      expect(onEvent).toHaveBeenCalledTimes(1);
      expect(onEvent).toHaveBeenCalledWith({ id: '1', type: 'message', data: { split: 2 } });
    });

    it('flushes a block terminated by CRLF CRLF', async () => {
      const { client, onEvent } = makeClient();
      await client.connect();

      xhrAt(0).emit('id: 1\r\nevent: message\r\ndata: {"a":1}\r\n\r\n');

      expect(onEvent).toHaveBeenCalledWith({ id: '1', type: 'message', data: { a: 1 } });
    });

    it('accepts a data line that omits the optional space after the colon', async () => {
      const { client, onEvent } = makeClient();
      await client.connect();

      xhrAt(0).emit('id: 1\nevent: message\ndata:{"a":1}\n\n');

      expect(onEvent).toHaveBeenCalledWith({ id: '1', type: 'message', data: { a: 1 } });
    });

    it('does not treat a comment line as a field', async () => {
      const { client, onEvent } = makeClient();
      await client.connect();

      xhrAt(0).emit(': keep-alive\nid: 1\ndata: {"a":1}\n\n');

      expect(onEvent).toHaveBeenCalledWith({ id: '1', type: 'message', data: { a: 1 } });
    });
  });

  describe('failure injection', () => {
    it('invokes onError and schedules a reconnect when the transport reports an error', async () => {
      const { client, onError } = makeClient();
      await client.connect();

      xhrAt(0).triggerError();

      expect(onError).toHaveBeenCalledTimes(1);
      expect(onError.mock.calls[0]?.[0]).toBeInstanceOf(Error);

      await jest.advanceTimersByTimeAsync(30_000);
      expect(FakeXHR.instances.length).toBe(2);
    });

    it('schedules a reconnect when the stream ends without an error firing', async () => {
      const { client } = makeClient();
      await client.connect();

      xhrAt(0).triggerLoadEnd();

      await jest.advanceTimersByTimeAsync(30_000);
      expect(FakeXHR.instances.length).toBe(2);
    });

    it('does not double-schedule a reconnect when error and loadend both fire for one failure', async () => {
      jest.spyOn(Math, 'random').mockReturnValue(0);
      const { client } = makeClient();
      await client.connect();

      const first = xhrAt(0);
      first.triggerError();
      first.triggerLoadEnd();

      await jest.advanceTimersByTimeAsync(999);
      expect(FakeXHR.instances.length).toBe(1);
      await jest.advanceTimersByTimeAsync(1);
      expect(FakeXHR.instances.length).toBe(2);

      const second = xhrAt(1);
      second.triggerError();
      await jest.advanceTimersByTimeAsync(1_999);
      expect(FakeXHR.instances.length).toBe(2);
      await jest.advanceTimersByTimeAsync(1);
      expect(FakeXHR.instances.length).toBe(3);
    });

    it('forces an immediate reconnect, bypassing backoff, once the buffered response exceeds MAX_RESPONSE_BYTES', async () => {
      const { client } = makeClient();
      await client.connect();

      xhrAt(0).emit('x'.repeat(MAX_RESPONSE_BYTES));
      await flush();

      expect(FakeXHR.instances.length).toBe(2);
    });

    it('retries after a null token so a sign-in during the session still connects', async () => {
      const getToken = jest.fn<Promise<string | null>, []>().mockResolvedValue(null);
      const { client } = makeClient(getToken);

      await client.connect();
      expect(FakeXHR.instances.length).toBe(0);

      getToken.mockResolvedValue('token-after-sign-in');
      await jest.advanceTimersByTimeAsync(30_000);

      expect(FakeXHR.instances.length).toBe(1);
    });

    it('reports a rejected getToken() and schedules a retry instead of rejecting', async () => {
      const getToken = jest
        .fn<Promise<string | null>, []>()
        .mockRejectedValue(new Error('secure store unavailable'));
      const { client, onError } = makeClient(getToken);

      await expect(client.connect()).resolves.toBeUndefined();

      expect(onError).toHaveBeenCalledWith(expect.objectContaining({ message: 'secure store unavailable' }));
      expect(FakeXHR.instances.length).toBe(0);

      getToken.mockResolvedValue('token-after-recovery');
      await jest.advanceTimersByTimeAsync(30_000);

      expect(FakeXHR.instances.length).toBe(1);
    });
  });

  describe('concurrency and ordering', () => {
    it('ignores frames on the xhr superseded by a second overlapping connect()', async () => {
      const { client, onEvent } = makeClient();

      const first = client.connect();
      const second = client.connect();
      await Promise.all([first, second]);

      expect(FakeXHR.instances.length).toBe(2);
      const stale = xhrAt(0);
      const active = xhrAt(1);

      stale.emit(block({ id: '1', data: { from: 'stale' } }));
      expect(onEvent).not.toHaveBeenCalled();

      active.emit(block({ id: '2', data: { from: 'active' } }));
      expect(onEvent).toHaveBeenCalledTimes(1);
      expect(onEvent).toHaveBeenCalledWith({ id: '2', type: 'message', data: { from: 'active' } });
    });

    it('delivers no more events after disconnect(), even if the network still emits', async () => {
      const { client, onEvent } = makeClient();
      await client.connect();
      const xhr = xhrAt(0);

      client.disconnect();
      xhr.emit(block({ id: '1', data: { a: 1 } }));

      expect(onEvent).not.toHaveBeenCalled();
    });

    it('clears a pending reconnect timer on disconnect(), so a teardown never races a reconnect', async () => {
      const { client } = makeClient();
      await client.connect();
      xhrAt(0).triggerError();

      client.disconnect();
      await jest.advanceTimersByTimeAsync(60_000);

      expect(FakeXHR.instances.length).toBe(1);
    });

    it('does not connect when dispose() happens while the token fetch is still in flight', async () => {
      let resolveToken!: (value: string | null) => void;
      const getToken = jest.fn<Promise<string | null>, []>(
        () => new Promise<string | null>((resolve) => (resolveToken = resolve)),
      );
      const { client } = makeClient(getToken);

      const connectPromise = client.connect();
      client.dispose();
      resolveToken('token-1');
      await connectPromise;

      expect(FakeXHR.instances.length).toBe(0);
    });
  });

  describe('timing and dwell', () => {
    it('jitters the reconnect delay into [base, 2*base) rather than firing at base', async () => {
      jest.spyOn(Math, 'random').mockReturnValue(0.99);
      const { client } = makeClient();
      await client.connect();

      xhrAt(0).triggerError();

      await jest.advanceTimersByTimeAsync(1_000);
      expect(FakeXHR.instances.length).toBe(1);

      await jest.advanceTimersByTimeAsync(990);
      expect(FakeXHR.instances.length).toBe(2);
    });

    it('does not re-arm the watchdog on a progress event that carried no bytes', async () => {
      const { client } = makeClient();
      await client.connect();

      await jest.advanceTimersByTimeAsync(HEARTBEAT_WATCHDOG_MS - 1_000);
      xhrAt(0).emit('');

      await jest.advanceTimersByTimeAsync(1_000);

      expect(FakeXHR.instances.length).toBe(2);
    });

    it('forces a reconnect exactly at the watchdog timeout, not before', async () => {
      const { client } = makeClient();
      await client.connect();

      await jest.advanceTimersByTimeAsync(HEARTBEAT_WATCHDOG_MS - 1);
      expect(FakeXHR.instances.length).toBe(1);

      await jest.advanceTimersByTimeAsync(1);
      expect(FakeXHR.instances.length).toBe(2);
    });

    it('resets the watchdog window on data receipt, deferring the forced reconnect', async () => {
      const { client } = makeClient();
      await client.connect();

      await jest.advanceTimersByTimeAsync(59_000);
      expect(FakeXHR.instances.length).toBe(1);

      xhrAt(0).emit(block({ id: '1', data: { a: 1 } }));

      await jest.advanceTimersByTimeAsync(59_000);
      expect(FakeXHR.instances.length).toBe(1);

      await jest.advanceTimersByTimeAsync(1_000);
      expect(FakeXHR.instances.length).toBe(2);
    });

    it('reconnects at the exact base backoff delay after the first failure', async () => {
      jest.spyOn(Math, 'random').mockReturnValue(0);
      const { client } = makeClient();
      await client.connect();

      xhrAt(0).triggerError();

      await jest.advanceTimersByTimeAsync(999);
      expect(FakeXHR.instances.length).toBe(1);
      await jest.advanceTimersByTimeAsync(1);
      expect(FakeXHR.instances.length).toBe(2);
    });

    it('doubles the backoff on consecutive failures and resets it after a successful data receipt', async () => {
      jest.spyOn(Math, 'random').mockReturnValue(0);
      const { client } = makeClient();
      await client.connect();

      xhrAt(0).triggerError();
      await jest.advanceTimersByTimeAsync(1_000);
      expect(FakeXHR.instances.length).toBe(2);

      xhrAt(1).triggerError();
      await jest.advanceTimersByTimeAsync(1_999);
      expect(FakeXHR.instances.length).toBe(2);
      await jest.advanceTimersByTimeAsync(1);
      expect(FakeXHR.instances.length).toBe(3);

      xhrAt(2).emit(block({ id: '1', data: { a: 1 } }));
      xhrAt(2).triggerError();
      await jest.advanceTimersByTimeAsync(999);
      expect(FakeXHR.instances.length).toBe(3);
      await jest.advanceTimersByTimeAsync(1);
      expect(FakeXHR.instances.length).toBe(4);
    });

    it('caps the backoff delay at MAX_RECONNECT_MS after repeated failures', async () => {
      jest.spyOn(Math, 'random').mockReturnValue(0);
      const { client } = makeClient();
      await client.connect();

      for (let i = 0; i < 5; i++) {
        xhrAt(FakeXHR.instances.length - 1).triggerError();
        await jest.advanceTimersByTimeAsync(30_000);
      }
      expect(FakeXHR.instances.length).toBe(6);

      xhrAt(5).triggerError();
      await jest.advanceTimersByTimeAsync(29_999);
      expect(FakeXHR.instances.length).toBe(6);
      await jest.advanceTimersByTimeAsync(1);
      expect(FakeXHR.instances.length).toBe(7);
    });
  });

  describe('resume via Last-Event-ID', () => {
    it('sends no Last-Event-ID header before any id has been seen', async () => {
      const { client } = makeClient();
      await client.connect();

      expect(xhrAt(0).requestHeaders['Last-Event-ID']).toBeUndefined();
    });

    it('sends the last seen id as Last-Event-ID on reconnect', async () => {
      jest.spyOn(Math, 'random').mockReturnValue(0);
      const { client } = makeClient();
      await client.connect();

      xhrAt(0).emit(block({ id: 'evt-42', data: { a: 1 } }));
      xhrAt(0).triggerError();

      await jest.advanceTimersByTimeAsync(1_000);
      expect(xhrAt(1).requestHeaders['Last-Event-ID']).toBe('evt-42');
    });
  });
});
