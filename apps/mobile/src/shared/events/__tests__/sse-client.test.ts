/**
 * SSEClient — XHR-based SSE parsing + reconnect/dispose lifecycle.
 *
 * XMLHttpRequest is replaced with a fake that lets the test drive the
 * progressive `responseText` the real client parses, so no network is needed.
 */

import { SSEClient } from '../sse-client';

type XhrHandler = (() => void) | null;

class FakeXHR {
  static instances: FakeXHR[] = [];
  onprogress: XhrHandler = null;
  onerror: XhrHandler = null;
  onloadend: XhrHandler = null;
  responseText = '';
  headers: Record<string, string> = {};
  method = '';
  url = '';
  aborted = false;
  sent = false;

  constructor() {
    FakeXHR.instances.push(this);
  }
  open(method: string, url: string): void {
    this.method = method;
    this.url = url;
  }
  setRequestHeader(key: string, value: string): void {
    this.headers[key] = value;
  }
  send(): void {
    this.sent = true;
  }
  abort(): void {
    this.aborted = true;
  }
  // Drive a progressive response chunk the way the platform XHR would.
  push(text: string): void {
    this.responseText += text;
    this.onprogress?.();
  }
}

describe('SSEClient', () => {
  const realXHR = (global as { XMLHttpRequest?: unknown }).XMLHttpRequest;

  beforeEach(() => {
    FakeXHR.instances = [];
    (global as { XMLHttpRequest: unknown }).XMLHttpRequest = FakeXHR;
  });
  afterEach(() => {
    (global as { XMLHttpRequest: unknown }).XMLHttpRequest = realXHR;
  });

  async function connect(onEvent = jest.fn()): Promise<{
    client: SSEClient;
    onEvent: jest.Mock;
    xhr: FakeXHR;
  }> {
    const client = new SSEClient('http://api/v1/events', async () => 'tok', onEvent, () => {});
    await client.connect();
    return { client, onEvent, xhr: FakeXHR.instances[0]! };
  }

  it('sends the bearer token and accepts the event stream', async () => {
    const { xhr, client } = await connect();
    expect(xhr.headers.Authorization).toBe('Bearer tok');
    expect(xhr.headers.Accept).toBe('text/event-stream');
    client.dispose();
  });

  it('parses a complete SSE event', async () => {
    const { xhr, onEvent, client } = await connect();
    xhr.push('id: 1\nevent: track_acquisition_completed\ndata: {"track_id":"t1"}\n\n');
    expect(onEvent).toHaveBeenCalledWith({
      id: '1',
      type: 'track_acquisition_completed',
      data: { track_id: 't1' },
    });
    client.dispose();
  });

  it('parses an event split across progress chunks', async () => {
    const { xhr, onEvent, client } = await connect();
    xhr.push('id: 2\nevent: track_acquisition_failed\nda');
    xhr.push('ta: {"track_id":"t2","reason":"boom"}\n\n');
    expect(onEvent).toHaveBeenCalledTimes(1);
    expect(onEvent).toHaveBeenCalledWith({
      id: '2',
      type: 'track_acquisition_failed',
      data: { track_id: 't2', reason: 'boom' },
    });
    client.dispose();
  });

  it('replays from the last event id on reconnect', async () => {
    const { xhr, client } = await connect();
    xhr.push('id: 7\nevent: track_acquisition_completed\ndata: {}\n\n');
    await client.connect(); // reconnect
    expect(FakeXHR.instances[0]!.aborted).toBe(true);
    expect(FakeXHR.instances[1]!.headers['Last-Event-ID']).toBe('7');
    client.dispose();
  });

  it('does not reconnect after dispose', async () => {
    const { xhr, client } = await connect();
    client.dispose();
    expect(xhr.aborted).toBe(true);
    const count = FakeXHR.instances.length;
    await client.connect();
    expect(FakeXHR.instances.length).toBe(count);
  });

  it('opens no connection without a token', async () => {
    const client = new SSEClient('http://api/v1/events', async () => null, jest.fn(), () => {});
    await client.connect();
    expect(FakeXHR.instances).toHaveLength(0);
    client.dispose();
  });
});
