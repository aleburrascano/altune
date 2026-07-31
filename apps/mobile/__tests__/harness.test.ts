import { Directory, File, Paths, __fs } from 'expo-file-system';
import * as SecureStore from 'expo-secure-store';
import TrackPlayer, { __player } from 'react-native-track-player';

const { __http } = require('../jest/doubles/fetch.js') as {
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

const { __secureStore } = SecureStore as unknown as {
  __secureStore: { read(key: string): string | undefined; failNext(op: string, e?: Error): void };
};

describe('filesystem double', () => {
  it('round-trips a write through a read', () => {
    const dir = new Directory(Paths.document, 'telemetry');
    dir.create();
    const file = new File(dir, 'outbox.json');

    file.write('["queued"]');

    expect(file.exists).toBe(true);
    expect(file.textSync()).toBe('["queued"]');
  });

  it('reports a file as absent until it is written', () => {
    const file = new File(Paths.document, 'nothing-here.json');
    expect(file.exists).toBe(false);
    expect(() => file.textSync()).toThrow(/ENOENT/);
  });

  it('forgets contents after delete, so a lost write is observable', () => {
    const file = new File(Paths.document, 'pref.json');
    file.write('"dark"');
    file.delete();
    expect(file.exists).toBe(false);
  });

  it('lists only direct children, as File instances', () => {
    const dir = new Directory(Paths.document, 'offline-audio');
    dir.create();
    new File(dir, 't1.mp3').write('audio');
    new File(dir, 't10.mp3').write('audio');

    const entries = dir.list();

    expect(entries).toHaveLength(2);
    expect(entries.every((entry) => entry instanceof File)).toBe(true);
    expect(entries.map((entry) => entry.uri.split('/').pop()).sort()).toEqual([
      't1.mp3',
      't10.mp3',
    ]);
  });

  it('injects a write failure exactly once', () => {
    const file = new File(Paths.document, 'outbox.json');
    __fs.failNext('write', new Error('disk full'));

    expect(() => file.write('["dropped"]')).toThrow('disk full');
    expect(file.exists).toBe(false);

    file.write('["kept"]');
    expect(file.textSync()).toBe('["kept"]');
  });

  it('injects a listing failure on a directory that exists, distinct from an absent one', () => {
    const dir = new Directory(Paths.document, 'offline-audio');
    dir.create();
    new File(dir, 't1.mp3').write('audio');
    __fs.failNext('list', new Error('EIO: i/o error'));

    expect(() => dir.list()).toThrow('EIO: i/o error');
    expect(dir.exists).toBe(true);
    expect(dir.list()).toHaveLength(1);
  });

  it('resets between tests', () => {
    expect(Object.keys(__fs.allFiles())).toHaveLength(0);
  });
});

describe('secure-store double', () => {
  it('round-trips a token', async () => {
    await SecureStore.setItemAsync('session', 'token-abc');
    await expect(SecureStore.getItemAsync('session')).resolves.toBe('token-abc');
  });

  it('returns null for an absent key rather than throwing', async () => {
    await expect(SecureStore.getItemAsync('missing')).resolves.toBeNull();
  });

  it('injects a keychain failure', async () => {
    __secureStore.failNext('set', new Error('keychain unavailable'));
    await expect(SecureStore.setItemAsync('session', 'token')).rejects.toThrow(
      'keychain unavailable',
    );
    expect(__secureStore.read('session')).toBeUndefined();
  });
});

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

describe('track-player double', () => {
  it('exposes a stable mock per method, so call assertions hold', async () => {
    expect(TrackPlayer.play).toBe(TrackPlayer.play);

    await TrackPlayer.play();

    expect(TrackPlayer.play).toHaveBeenCalledTimes(1);
  });

  it('injects a rejection on a named method', async () => {
    __player.failNext('skipToNext', new Error('player not initialised'));
    await expect(TrackPlayer.skipToNext()).rejects.toThrow('player not initialised');
  });

  it('drives playback state and progress', () => {
    __player.setState('playing');
    __player.setProgress({ position: 42, duration: 180 });

    const { usePlaybackState, useProgress } = require('react-native-track-player');

    expect(usePlaybackState().state).toBe('playing');
    expect(useProgress()).toEqual({ position: 42, duration: 180, buffered: 0 });
  });

  it('resets call counts between tests', () => {
    expect(TrackPlayer.play).toHaveBeenCalledTimes(0);
  });
});
