import { SSEClient, type StreamRequest } from '@shared/events';

class HeaderRecordingRequest implements StreamRequest {
  responseText = '';
  status = 0;
  headers: [string, string][] = [];
  onprogress = null;
  onerror = null;
  onloadend = null;

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

describe('SSEClient on web', () => {
  it('omits the correlation header, where the API CORS policy would reject the preflight', async () => {
    const request = new HeaderRecordingRequest();

    await new SSEClient(
      'https://api.example.com/v1/events',
      async () => 'token-1',
      () => undefined,
      () => undefined,
      { openRequest: () => request, random: () => 0 },
    ).connect();

    expect(request.headers).toContainEqual(['Authorization', 'Bearer token-1']);
    expect(request.headers.map(([name]) => name)).not.toContain('X-Correlation-ID');
  });
});
