import { QueryClient } from '@tanstack/react-query';

import {
  startServerEvents,
  type AppForeground,
  type ServerEvent,
  type ServerEventsClientFactory,
} from '@shared/events';
import { runSignOutCleanups } from '@shared/session';
import { createMemoryFileStore } from '@testing/fakes/files/inMemoryFileStore';
import { applyKillSwitches, setKillSwitchFileStore } from '@shared/killSwitch';
import { playlistKeys } from '@shared/query';

const EVENTS_ENDPOINT = /\/v1\/events$/;
const TWICE = 2;

interface FakeClient {
  url: string;
  getToken: () => Promise<string | null>;
  onEvent: (event: ServerEvent) => void;
  onError: (error: unknown) => void;
  connect: jest.Mock;
  disconnect: jest.Mock;
  dispose: jest.Mock;
}

interface FakeForeground extends AppForeground {
  emit(state: string): void;
}

function fakeForeground(): FakeForeground {
  let current = 'active';
  const listeners = new Set<(state: string) => void>();
  return {
    isActive: () => current === 'active',
    subscribe(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    emit(state) {
      current = state;
      [...listeners].forEach((listener) => listener(state));
    },
  };
}

interface Started {
  clients: FakeClient[];
  warnings: [string, unknown][];
  foreground: FakeForeground;
  stop: () => void;
  client(index: number): FakeClient;
}

let running: (() => void) | null = null;

function start({
  cache = new QueryClient(),
  getToken = async () => 'token-1',
}: {
  cache?: QueryClient;
  getToken?: () => Promise<string | null>;
} = {}): Started {
  const clients: FakeClient[] = [];
  const warnings: [string, unknown][] = [];
  const foreground = fakeForeground();
  function createClient(...[url, token, onEvent, onError]: Parameters<ServerEventsClientFactory>) {
    const client: FakeClient = {
      url,
      getToken: token,
      onEvent,
      onError,
      connect: jest.fn(),
      disconnect: jest.fn(),
      dispose: jest.fn(),
    };
    clients.push(client);
    return client;
  }
  const stop = startServerEvents({
    cache,
    createClient,
    getToken,
    foreground,
    warn: (message, detail) => warnings.push([message, detail]),
  });
  running = stop;
  function client(index: number): FakeClient {
    const found = clients[index];
    if (!found) throw new Error(`no fake client at index ${index}`);
    return found;
  }
  return { clients, warnings, foreground, stop, client };
}

function stop(started: Started): void {
  started.stop();
  running = null;
}

beforeEach(() => {
  setKillSwitchFileStore(createMemoryFileStore());
});

afterEach(() => {
  running?.();
  running = null;
  setKillSwitchFileStore();
});

describe('startServerEvents', () => {
  it('connects a single client to the events endpoint', () => {
    const { clients, client } = start();

    expect(clients).toHaveLength(1);
    expect(client(0).url).toMatch(EVENTS_ENDPOINT);
    expect(client(0).connect).toHaveBeenCalledTimes(1);
  });

  it('applies a received resync event to the cache, marking its cached playlists stale', () => {
    const cache = new QueryClient();
    cache.setQueryData(playlistKeys.list, [{ id: 'playlist-1' }]);
    const { client, warnings } = start({ cache });

    client(0).onEvent({ id: '1', type: 'resync', data: {} });

    expect(cache.getQueryState(playlistKeys.list)?.isInvalidated).toBe(true);
    expect(warnings).toEqual([]);
  });

  it('warns with the captured type on every unrecognized event, not only the first', () => {
    const { client, warnings } = start();

    client(0).onEvent({ id: '1', type: 'mystery_event', data: {} });
    client(0).onEvent({ id: '2', type: 'mystery_event', data: {} });
    client(0).onEvent({ id: '3', type: 'another_future_event', data: {} });

    expect(warnings).toEqual([
      ['[sse] unrecognized event type', { type: 'mystery_event' }],
      ['[sse] unrecognized event type', { type: 'mystery_event' }],
      ['[sse] unrecognized event type', { type: 'another_future_event' }],
    ]);
  });

  it('warns about a transport error without touching the cache', () => {
    const cache = new QueryClient();
    cache.setQueryData(playlistKeys.list, [{ id: 'playlist-1' }]);
    const { client, warnings } = start({ cache });
    const error = new Error('boom');

    expect(() => client(0).onError(error)).not.toThrow();

    expect(warnings).toEqual([['[sse]', error]]);
    expect(cache.getQueryState(playlistKeys.list)?.isInvalidated).toBe(false);
    expect(cache.getQueryData(playlistKeys.list)).toEqual([{ id: 'playlist-1' }]);
  });

  it('disconnects when the app goes to the background', () => {
    const { client, foreground } = start();

    foreground.emit('background');

    expect(client(0).disconnect).toHaveBeenCalledTimes(1);
    expect(client(0).connect).toHaveBeenCalledTimes(1);
  });

  it('disconnects on any non-active state, not only background', () => {
    const { client, foreground } = start();

    foreground.emit('inactive');

    expect(client(0).disconnect).toHaveBeenCalledTimes(1);
  });

  it('reconnects when the app returns to active', () => {
    const { client, foreground } = start();

    foreground.emit('background');
    foreground.emit('active');

    expect(client(0).connect).toHaveBeenCalledTimes(TWICE);
  });

  it('disposes the client and stops reacting to app state changes once stopped', () => {
    const started = start();

    stop(started);
    expect(started.client(0).dispose).toHaveBeenCalledTimes(1);

    started.foreground.emit('background');

    expect(started.client(0).disconnect).not.toHaveBeenCalled();
  });

  describe('identity changes', () => {
    it('disposes the stream the previous user opened and connects a fresh one', () => {
      const { clients, client } = start();

      runSignOutCleanups();

      expect(client(0).dispose).toHaveBeenCalledTimes(1);
      expect(clients).toHaveLength(TWICE);
      expect(client(1).connect).toHaveBeenCalledTimes(1);
    });

    it('opens the fresh stream without connecting while the app is in the background', () => {
      const { clients, client, foreground } = start();
      foreground.emit('background');

      runSignOutCleanups();

      expect(clients).toHaveLength(TWICE);
      expect(client(1).connect).not.toHaveBeenCalled();
    });

    it('stops opening streams for identity changes once stopped', () => {
      const started = start();

      stop(started);
      runSignOutCleanups();

      expect(started.clients).toHaveLength(1);
    });
  });

  describe('token resolution passed to the transport', () => {
    it('resolves the token the app looks up', async () => {
      const { client } = start({ getToken: async () => 'token-abc' });

      await expect(client(0).getToken()).resolves.toBe('token-abc');
    });

    it('resolves null when there is no active session', async () => {
      const { client } = start({ getToken: async () => null });

      await expect(client(0).getToken()).resolves.toBeNull();
    });

    it('resolves null rather than rejecting when the lookup fails', async () => {
      const { client } = start({ getToken: () => Promise.reject(new Error('network down')) });

      await expect(client(0).getToken()).resolves.toBeNull();
    });
  });
});

describe('remote kill switch', () => {
  function switchServerEvents(enabled: boolean): void {
    applyKillSwitches({ sse_enabled: enabled });
  }

  it('does not connect at start while the switch is off', () => {
    switchServerEvents(false);

    const { client } = start();

    expect(client(0).connect).not.toHaveBeenCalled();
  });

  it('does not reconnect on a return to the foreground while the switch is off', () => {
    switchServerEvents(false);
    const { client, foreground } = start();

    foreground.emit('background');
    foreground.emit('active');

    expect(client(0).connect).not.toHaveBeenCalled();
  });

  it('drops a live connection when the switch is turned off, and reconnects when it is turned back on', () => {
    const { client } = start();
    expect(client(0).connect).toHaveBeenCalledTimes(1);

    switchServerEvents(false);
    expect(client(0).disconnect).toHaveBeenCalledTimes(1);

    switchServerEvents(true);
    expect(client(0).connect).toHaveBeenCalledTimes(TWICE);
  });

  it('waits for the foreground to reconnect when the switch comes back on in the background', () => {
    switchServerEvents(false);
    const { client, foreground } = start();
    foreground.emit('background');

    switchServerEvents(true);
    expect(client(0).connect).not.toHaveBeenCalled();

    foreground.emit('active');
    expect(client(0).connect).toHaveBeenCalledTimes(1);
  });

  it('ignores the other loops’ switches and stops listening once stopped', () => {
    const started = start();

    applyKillSwitches({ telemetry_enabled: false, offline_downloads_enabled: false });
    expect(started.client(0).disconnect).not.toHaveBeenCalled();

    stop(started);
    switchServerEvents(false);
    switchServerEvents(true);
    expect(started.client(0).connect).toHaveBeenCalledTimes(1);
  });
});
