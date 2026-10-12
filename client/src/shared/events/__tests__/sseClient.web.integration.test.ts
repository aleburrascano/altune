import { SSEClient } from '@shared/events/sseClient';

describe('SSEClient on web', () => {
  it('omits the correlation header, where the API CORS policy would reject the preflight', async () => {
    const setHeader = jest.spyOn(XMLHttpRequest.prototype, 'setRequestHeader');
    jest.spyOn(XMLHttpRequest.prototype, 'open').mockImplementation(() => undefined);
    jest.spyOn(XMLHttpRequest.prototype, 'send').mockImplementation(() => undefined);
    setHeader.mockImplementation(() => undefined);

    await new SSEClient(
      'https://api.example.com/v1/events',
      async () => 'token-1',
      jest.fn(),
      jest.fn(),
    ).connect();

    expect(setHeader).toHaveBeenCalledWith('Authorization', 'Bearer token-1');
    expect(setHeader).not.toHaveBeenCalledWith('X-Correlation-ID', expect.anything());
  });
});
