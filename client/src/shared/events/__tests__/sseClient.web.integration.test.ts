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

describe('SSEClient on web', () => {
  afterEach(() => {
    jest.useRealTimers();
  });

  it('omits the correlation header, where the API CORS policy would reject the preflight', async () => {
    const request = new HeaderRecordingRequest();

    await connectedClient(request, () => undefined);

    expect(request.headers).toContainEqual(['Authorization', 'Bearer token-1']);
    expect(request.headers.map(([name]) => name)).not.toContain('X-Correlation-ID');
  });

  it('reports a connection error without a correlation id', async () => {
    jest.useFakeTimers();
    const request = new HeaderRecordingRequest();
    const errors: unknown[] = [];
    const client = await connectedClient(request, (error) => errors.push(error));

    request.onerror?.();
    client.dispose();

    expect(errors).toEqual([
      expect.objectContaining({ correlationId: null, message: 'SSE connection error' }),
    ]);
  });

  it('reports a refused stream without a correlation id', async () => {
    jest.useFakeTimers();
    const request = new HeaderRecordingRequest();
    const errors: unknown[] = [];
    const client = await connectedClient(request, (error) => errors.push(error));

    request.status = BAD_GATEWAY;
    request.onloadend?.();
    client.dispose();

    expect(errors).toEqual([
      expect.objectContaining({ correlationId: null, message: 'SSE stream refused (status=502)' }),
    ]);
  });
});
