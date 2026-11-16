import { createQueryClient, transientRetryOptions } from '@shared/query';

describe('createQueryClient()', () => {
  it('gives every query the app-wide retry policy and backoff', () => {
    const queries = createQueryClient().getDefaultOptions().queries;

    expect(queries?.retry).toBe(transientRetryOptions.retry);
    expect(queries?.retryDelay).toBe(transientRetryOptions.retryDelay);
  });

  it('serves a read from the cache for 30 seconds, then fetches again', async () => {
    const FRESH_FOR_MS = 30_000;
    jest.useFakeTimers({ now: 0 });
    try {
      const client = createQueryClient();
      let fetches = 0;
      async function fetchAlbums(): Promise<string> {
        fetches += 1;
        return 'albums';
      }
      function read(): Promise<string> {
        return client.fetchQuery({ queryKey: ['albums'], queryFn: fetchAlbums });
      }

      await read();
      jest.setSystemTime(FRESH_FOR_MS - 1);
      await read();
      expect(fetches).toBe(1);

      jest.setSystemTime(FRESH_FOR_MS + 1);
      await read();
      expect(fetches).toBe(1 + 1);
      client.clear();
    } finally {
      jest.useRealTimers();
    }
  });
});
