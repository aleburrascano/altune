const { __http } = require('../fetch.js') as {
  __http: {
    reply(spec: string, response?: { status?: number; json?: unknown; malformed?: boolean }): void;
    replyOnce(
      spec: string,
      response?: { status?: number; json?: unknown; malformed?: boolean },
    ): void;
    hang(spec: string): void;
    fail(spec: string, error?: Error): void;
    last(): { method: string; path: string; query: string; headers: Record<string, string> };
    requests: unknown[];
  };
};

describe('http double', () => {
  it('answers a registered route and records the request that reached it', async () => {
    __http.reply('GET /v1/tracks', { status: 200, json: { items: [], total: 0 } });

    const response = await fetch('http://api.test/v1/tracks?limit=20', {
      headers: { Authorization: 'Bearer t' },
    });

    expect(response.status).toBe(200);
    await expect(response.json()).resolves.toEqual({ items: [], total: 0 });
    expect(__http.last()).toMatchObject({
      method: 'GET',
      path: '/v1/tracks',
      query: 'limit=20',
      headers: { Authorization: 'Bearer t' },
    });
  });

  it('throws rather than silently succeeding when no rule matches', async () => {
    await expect(fetch('http://api.test/v1/unregistered')).rejects.toThrow(
      'no rule registered for GET /v1/unregistered',
    );
  });

  it('discriminates by method, so a wrong verb is not answered', async () => {
    __http.reply('DELETE /v1/playlists/p1', { status: 204 });

    await expect(fetch('http://api.test/v1/playlists/p1', { method: 'PATCH' })).rejects.toThrow(
      'no rule registered for PATCH /v1/playlists/p1',
    );
  });

  it('marks a non-2xx response as not ok', async () => {
    __http.reply('GET /v1/tracks', { status: 500 });

    const response = await fetch('http://api.test/v1/tracks');

    expect(response.ok).toBe(false);
    expect(response.status).toBe(500);
  });

  it('injects a transport failure, distinct from any status', async () => {
    __http.fail('GET /v1/tracks');

    await expect(fetch('http://api.test/v1/tracks')).rejects.toThrow(TypeError);
  });

  it('injects a truncated body that parses as a SyntaxError, not as data', async () => {
    __http.reply('GET /v1/tracks', { status: 200, malformed: true });

    const response = await fetch('http://api.test/v1/tracks');

    expect(response.ok).toBe(true);
    await expect(response.json()).rejects.toThrow(SyntaxError);
  });

  it('hangs until the signal aborts, and then rejects with an AbortError', async () => {
    __http.hang('GET /v1/tracks');
    const controller = new AbortController();
    const pending = fetch('http://api.test/v1/tracks', { signal: controller.signal });
    let settled = false;
    void pending.catch(() => {
      settled = true;
    });

    await Promise.resolve();
    expect(settled).toBe(false);

    controller.abort();

    await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
  });

  it('rejects immediately when the signal is already aborted', async () => {
    __http.reply('GET /v1/tracks', { status: 200, json: {} });
    const controller = new AbortController();
    controller.abort();

    await expect(
      fetch('http://api.test/v1/tracks', { signal: controller.signal }),
    ).rejects.toMatchObject({ name: 'AbortError' });
  });

  it('consumes a replyOnce rule so a second call falls through', async () => {
    __http.replyOnce('GET /v1/tracks', { status: 200, json: { items: [] } });

    await fetch('http://api.test/v1/tracks');

    await expect(fetch('http://api.test/v1/tracks')).rejects.toThrow('no rule registered');
  });

  it('resets rules and recorded requests between tests', () => {
    expect(__http.requests).toHaveLength(0);
  });
});
