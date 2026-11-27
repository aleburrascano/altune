import { SSEClient, type StreamRequest } from '@shared/events';

const BAD_GATEWAY = 502;

type Listener = (() => void) | null;

class HeaderRecordingRequest implements StreamRequest {
  responseText = '';
  status = 0;
  headers: [string, string][] = [];
  onprogress: Listener = null;
  onerror: Listener = null;
  onloadend: Listener = null;

  open(): void {}

  setRequestHeader(name: string, value: string): void {
    this.headers.push([name, value]);
  }

  getResponseHeader(): string | null {
    return null;
  }

  send(): void {}

  abort(): void {}
}

async function connectedClient(request: StreamRequest, onError: (error: unknown) => void) {
  const client = new SSEClient(
    'https://api.example.com/v1/events',
    async () => 'token-1',
    () => undefined,
    onError,
    { openRequest: () => request, random: () => 0, warn: () => undefined },
  );
  await client.connect();
  return client;
}

const CORRELATION_ID_SHAPE = /^[0-9a-f]{16}$/;

describe('SSEClient on web', () => {
  afterEach(() => {
    jest.useRealTimers();
  });

  it('sends the correlation header, which the API CORS policy allows', async () => {
    const request = new HeaderRecordingRequest();

    await connectedClient(request, () => undefined);

    expect(request.headers).toContainEqual(['Authorization', 'Bearer token-1']);
    expect(request.headers.map(([name]) => name)).toContain('X-Correlation-ID');
  });

  it('reports a connection error with the correlation id it sent', async () => {
    jest.useFakeTimers();
    const request = new HeaderRecordingRequest();
    const errors: unknown[] = [];
    const client = await connectedClient(request, (error) => errors.push(error));

    request.onerror?.();
    client.dispose();

    expect(errors).toEqual([
      expect.objectContaining({
        correlationId: expect.stringMatching(CORRELATION_ID_SHAPE),
        message: expect.stringContaining('SSE connection error'),
      }),
    ]);
  });

  it('reports a refused stream with the correlation id it sent', async () => {
    jest.useFakeTimers();
    const request = new HeaderRecordingRequest();
    const errors: unknown[] = [];
    const client = await connectedClient(request, (error) => errors.push(error));

    request.status = BAD_GATEWAY;
    request.onloadend?.();
    client.dispose();

    expect(errors).toEqual([
      expect.objectContaining({
        correlationId: expect.stringMatching(CORRELATION_ID_SHAPE),
        message: expect.stringContaining('SSE stream refused (status=502'),
      }),
    ]);
  });
});
