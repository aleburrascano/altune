import { MAX_RESPONSE_BYTES, SSEClient } from '../sse-client';

type Handler = () => void;

class CapXHR {
  static instances: CapXHR[] = [];

  responseText = '';
  status = 200;
  onprogress: Handler | null = null;
  onerror: Handler | null = null;
  onloadend: Handler | null = null;

  constructor() {
    CapXHR.instances.push(this);
  }

  open(): void {}
  setRequestHeader(): void {}
  getResponseHeader(): string | null {
    return null;
  }
  send(): void {}
  abort(): void {}

  emit(chunk: string): void {
    this.responseText += chunk;
    this.onprogress?.();
  }
}

function makeClient() {
  return new SSEClient(
    'https://api.example.com/v1/events',
    jest.fn<Promise<string | null>, []>().mockResolvedValue('token-1'),
    jest.fn(),
    jest.fn(),
  );
}

function opened(index: number): CapXHR {
  const xhr = CapXHR.instances[index];
  if (!xhr) throw new Error(`no CapXHR at ${index}`);
  return xhr;
}

describe('SSEClient response cap reconnect', () => {
  beforeEach(() => {
    CapXHR.instances = [];
    (global as unknown as { XMLHttpRequest: unknown }).XMLHttpRequest = CapXHR;
    jest.useFakeTimers();
    jest.spyOn(Math, 'random').mockReturnValue(0);
  });

  afterEach(() => {
    jest.useRealTimers();
    jest.restoreAllMocks();
  });

  it('does not reconnect synchronously when the cap is hit without a terminator', async () => {
    const client = makeClient();
    await client.connect();

    opened(0).emit('x'.repeat(MAX_RESPONSE_BYTES));
    await Promise.resolve();

    expect(CapXHR.instances.length).toBe(1);
    await jest.advanceTimersByTimeAsync(1_000);
    expect(CapXHR.instances.length).toBe(2);
  });

  it('grows the delay while the same oversized block keeps stalling', async () => {
    const client = makeClient();
    await client.connect();

    opened(0).emit('x'.repeat(MAX_RESPONSE_BYTES));
    await jest.advanceTimersByTimeAsync(1_000);
    opened(1).emit('x'.repeat(MAX_RESPONSE_BYTES));

    await jest.advanceTimersByTimeAsync(1_999);
    expect(CapXHR.instances.length).toBe(2);
    await jest.advanceTimersByTimeAsync(1);
    expect(CapXHR.instances.length).toBe(3);
  });

  it('reconnects immediately when the stream advanced lastEventId', async () => {
    const client = makeClient();
    await client.connect();

    opened(0).emit(`id: 5\ndata: {}\n\n:${'x'.repeat(MAX_RESPONSE_BYTES)}`);
    await Promise.resolve();
    await Promise.resolve();

    expect(CapXHR.instances.length).toBe(2);
  });
});
