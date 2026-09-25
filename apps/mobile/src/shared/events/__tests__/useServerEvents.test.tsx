import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, create, type ReactTestRenderer } from 'react-test-renderer';

import {
  useServerEvents,
  type ServerEventsClient,
  type ServerEventsClientFactory,
} from '../useServerEvents';
import { SSEClient, type ServerEvent } from '../sse-client';
import { applyServerEvent } from '../applyServerEvent';
import { supabase } from '@shared/auth/supabaseClient';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';
import { createMemoryFileStore } from '@shared/files/__tests__/memoryFileStore';
import { applyKillSwitches, setKillSwitchFileStore } from '@shared/killSwitch/killSwitch';

jest.mock('../applyServerEvent', () => ({ applyServerEvent: jest.fn() }));

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

type AppStateChangeHandler = (state: string) => void;

jest.mock('react-native/Libraries/AppState/AppState', () => {
  const listeners: AppStateChangeHandler[] = [];
  return {
    default: {
      currentState: 'active',
      isAvailable: true,
      addEventListener: jest.fn((_type: string, handler: AppStateChangeHandler) => {
        listeners.push(handler);
        return {
          remove: jest.fn(() => {
            const index = listeners.indexOf(handler);
            if (index !== -1) listeners.splice(index, 1);
          }),
        };
      }),
    },
    __listeners: listeners,
  };
});

interface FakeSSEClient extends ServerEventsClient {
  url: string;
  getToken: () => Promise<string | null>;
  onEvent: (event: ServerEvent) => void;
  onError: (error: unknown) => void;
  connect: jest.Mock;
  disconnect: jest.Mock;
  dispose: jest.Mock;
}

const instances: FakeSSEClient[] = [];

const createFakeClient: ServerEventsClientFactory = (url, getToken, onEvent, onError) => {
  const instance: FakeSSEClient = {
    url,
    getToken,
    onEvent,
    onError,
    connect: jest.fn(),
    disconnect: jest.fn(),
    dispose: jest.fn(),
  };
  instances.push(instance);
  return instance;
};

function instanceAt(index: number): FakeSSEClient {
  const instance = instances[index];
  if (!instance) throw new Error(`no fake SSEClient instance at index ${index}`);
  return instance;
}

const { __listeners: appStateListeners } = jest.requireMock(
  'react-native/Libraries/AppState/AppState',
);

function emitAppStateChange(state: string): void {
  [...appStateListeners].forEach((handler) => handler(state));
}

function Harness({
  createClient = createFakeClient,
}: {
  createClient?: ServerEventsClientFactory;
}): null {
  useServerEvents(createClient);
  return null;
}

function DefaultHarness(): null {
  useServerEvents();
  return null;
}

let activeRenderer: ReactTestRenderer | null = null;

function mount(queryClient: QueryClient): ReactTestRenderer {
  let renderer!: ReactTestRenderer;
  act(() => {
    renderer = create(
      <QueryClientProvider client={queryClient}>
        <Harness />
      </QueryClientProvider>,
    );
  });
  activeRenderer = renderer;
  return renderer;
}

function rerenderWithSameClient(renderer: ReactTestRenderer, queryClient: QueryClient): void {
  act(() => {
    renderer.update(
      <QueryClientProvider client={queryClient}>
        <Harness />
      </QueryClientProvider>,
    );
  });
}

function unmount(renderer: ReactTestRenderer): void {
  act(() => {
    renderer.unmount();
  });
  activeRenderer = null;
}

describe('useServerEvents', () => {
  beforeEach(() => {
    instances.length = 0;
    jest.clearAllMocks();
  });

  afterEach(() => {
    if (activeRenderer) unmount(activeRenderer);
    appStateListeners.length = 0;
  });

  it('connects a single SSEClient to the events endpoint on mount', () => {
    mount(new QueryClient());

    expect(instances).toHaveLength(1);
    expect(instanceAt(0).url).toMatch(/\/v1\/events$/);
    expect(instanceAt(0).connect).toHaveBeenCalledTimes(1);
  });

  it('forwards a received server event unchanged to applyServerEvent with the active query client', () => {
    const queryClient = new QueryClient();
    mount(queryClient);
    const event: ServerEvent = { id: '1', type: 'resync', data: {} };

    act(() => {
      instanceAt(0).onEvent(event);
    });

    expect(applyServerEvent).toHaveBeenCalledWith(queryClient, event);
  });

  it('disconnects when the app goes to the background', () => {
    mount(new QueryClient());

    emitAppStateChange('background');

    expect(instanceAt(0).disconnect).toHaveBeenCalledTimes(1);
    expect(instanceAt(0).connect).toHaveBeenCalledTimes(1);
  });

  it('disconnects on any non-active state, not only background', () => {
    mount(new QueryClient());

    emitAppStateChange('inactive');

    expect(instanceAt(0).disconnect).toHaveBeenCalledTimes(1);
  });

  it('reconnects when the app returns to active', () => {
    mount(new QueryClient());

    emitAppStateChange('background');
    emitAppStateChange('active');

    expect(instanceAt(0).connect).toHaveBeenCalledTimes(2);
  });

  it('disposes the client and stops reacting to app state changes on unmount', () => {
    const renderer = mount(new QueryClient());

    unmount(renderer);
    expect(instanceAt(0).dispose).toHaveBeenCalledTimes(1);

    const disconnectCallsBeforeEmit = instanceAt(0).disconnect.mock.calls.length;
    emitAppStateChange('background');

    expect(instanceAt(0).disconnect.mock.calls.length).toBe(disconnectCallsBeforeEmit);
  });

  describe('identity changes', () => {
    it('disposes the stream the previous user opened and connects a fresh one', () => {
      mount(new QueryClient());

      runSignOutCleanups();

      expect(instanceAt(0).dispose).toHaveBeenCalledTimes(1);
      expect(instances).toHaveLength(2);
      expect(instanceAt(1).connect).toHaveBeenCalledTimes(1);
    });

    it('stops opening streams for identity changes once unmounted', () => {
      const renderer = mount(new QueryClient());

      unmount(renderer);
      runSignOutCleanups();

      expect(instances).toHaveLength(1);
    });
  });

  it('does not recreate the client on a re-render that leaves the query client unchanged', () => {
    const queryClient = new QueryClient();
    const renderer = mount(queryClient);

    rerenderWithSameClient(renderer, queryClient);

    expect(instances).toHaveLength(1);
    expect(instanceAt(0).dispose).not.toHaveBeenCalled();
  });

  it('swallows a transport error without forwarding it as a server event', () => {
    mount(new QueryClient());

    expect(() => instanceAt(0).onError(new Error('boom'))).not.toThrow();
    expect(applyServerEvent).not.toHaveBeenCalled();
  });

  describe('token resolution passed to the transport', () => {
    it('resolves the session access token', async () => {
      (supabase.auth.getSession as jest.Mock).mockResolvedValue({
        data: { session: { access_token: 'token-abc' } },
      });
      mount(new QueryClient());

      await expect(instanceAt(0).getToken()).resolves.toBe('token-abc');
    });

    it('resolves null when there is no active session', async () => {
      (supabase.auth.getSession as jest.Mock).mockResolvedValue({ data: { session: null } });
      mount(new QueryClient());

      await expect(instanceAt(0).getToken()).resolves.toBeNull();
    });

    it('resolves null rather than rejecting when getSession throws', async () => {
      (supabase.auth.getSession as jest.Mock).mockRejectedValue(new Error('network down'));
      mount(new QueryClient());

      await expect(instanceAt(0).getToken()).resolves.toBeNull();
    });
  });

  describe('default client factory', () => {
    it('constructs a real SSEClient and drives it through its lifecycle when no factory is injected', () => {
      const connect = jest.spyOn(SSEClient.prototype, 'connect').mockResolvedValue(undefined);
      const disconnect = jest.spyOn(SSEClient.prototype, 'disconnect').mockImplementation(() => {});
      const dispose = jest.spyOn(SSEClient.prototype, 'dispose').mockImplementation(() => {});

      let renderer!: ReactTestRenderer;
      act(() => {
        renderer = create(
          <QueryClientProvider client={new QueryClient()}>
            <DefaultHarness />
          </QueryClientProvider>,
        );
      });
      activeRenderer = renderer;

      expect(connect).toHaveBeenCalledTimes(1);
      expect(connect.mock.contexts[0]).toBeInstanceOf(SSEClient);

      emitAppStateChange('background');
      expect(disconnect).toHaveBeenCalledTimes(1);

      unmount(renderer);
      expect(dispose).toHaveBeenCalledTimes(1);

      connect.mockRestore();
      disconnect.mockRestore();
      dispose.mockRestore();
    });
  });
});

describe('token lookup deadline', () => {
  const getSession = supabase.auth.getSession as jest.Mock;

  function Harness(): null {
    useServerEvents();
    return null;
  }

  let renderer: ReactTestRenderer | null = null;

  beforeEach(() => {
    jest.useFakeTimers();
    jest.spyOn(Math, 'random').mockReturnValue(0);
    getSession.mockReset();
    getSession.mockReturnValue(new Promise(() => undefined));
  });

  afterEach(() => {
    act(() => {
      renderer?.unmount();
    });
    renderer = null;
    jest.restoreAllMocks();
    jest.useRealTimers();
  });

  async function advance(ms: number): Promise<void> {
    await act(async () => {
      jest.advanceTimersByTime(ms);
    });
  }

  describe('useServerEvents() when the token lookup never settles', () => {
    it('gives up on the lookup at 15000ms and schedules a reconnect that asks for a token again', async () => {
      act(() => {
        renderer = create(
          <QueryClientProvider client={new QueryClient()}>
            <Harness />
          </QueryClientProvider>,
        );
      });
      expect(getSession).toHaveBeenCalledTimes(1);

      await advance(14_999);
      await advance(1_000);
      expect(getSession).toHaveBeenCalledTimes(1);

      await advance(1);
      await advance(1_000);
      expect(getSession).toHaveBeenCalledTimes(2);
    });
  });
});

describe('remote kill switch', () => {
  // Regression for issue #955: the SSE connection must be gated by its remote kill switch, so a
  // reconnect storm can be stopped without an app release.
  type AppStateChangeHandler = (state: string) => void;

  const appStateMock = jest.requireMock('react-native/Libraries/AppState/AppState') as {
    default: { currentState: string };
    __listeners: AppStateChangeHandler[];
  };

  type FakeClient = { connect: jest.Mock; disconnect: jest.Mock; dispose: jest.Mock };

  let client: FakeClient;

  const createFakeClient: ServerEventsClientFactory = (): ServerEventsClient => client;

  function Harness(): null {
    useServerEvents(createFakeClient);
    return null;
  }

  let renderer: ReactTestRenderer | null = null;

  function mount(): void {
    act(() => {
      renderer = create(
        <QueryClientProvider client={new QueryClient()}>
          <Harness />
        </QueryClientProvider>,
      );
    });
  }

  function emitAppState(state: string): void {
    appStateMock.default.currentState = state;
    act(() => {
      [...appStateMock.__listeners].forEach((handler) => handler(state));
    });
  }

  function switchServerEvents(enabled: boolean): void {
    act(() => applyKillSwitches({ sse_enabled: enabled }));
  }

  beforeEach(() => {
    client = { connect: jest.fn(), disconnect: jest.fn(), dispose: jest.fn() };
    appStateMock.default.currentState = 'active';
    setKillSwitchFileStore(createMemoryFileStore());
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    if (renderer) act(() => renderer?.unmount());
    renderer = null;
    appStateMock.__listeners.length = 0;
    setKillSwitchFileStore();
    jest.restoreAllMocks();
  });

  describe('useServerEvents — remote kill switch', () => {
    it('does not connect on mount while the switch is off', () => {
      switchServerEvents(false);

      mount();

      expect(client.connect).not.toHaveBeenCalled();
    });

    it('does not reconnect on a return to the foreground while the switch is off', () => {
      switchServerEvents(false);
      mount();

      emitAppState('background');
      emitAppState('active');

      expect(client.connect).not.toHaveBeenCalled();
    });

    it('drops a live connection when the switch is turned off, and reconnects when it is turned back on', () => {
      mount();
      expect(client.connect).toHaveBeenCalledTimes(1);

      switchServerEvents(false);
      expect(client.disconnect).toHaveBeenCalledTimes(1);

      switchServerEvents(true);
      expect(client.connect).toHaveBeenCalledTimes(2);
    });

    it('waits for the foreground to reconnect when the switch comes back on in the background', () => {
      switchServerEvents(false);
      mount();
      emitAppState('background');

      switchServerEvents(true);
      expect(client.connect).not.toHaveBeenCalled();

      emitAppState('active');
      expect(client.connect).toHaveBeenCalledTimes(1);
    });

    it('ignores the other loops’ switches and stops listening once unmounted', () => {
      mount();

      act(() => applyKillSwitches({ telemetry_enabled: false, offline_downloads_enabled: false }));
      expect(client.disconnect).not.toHaveBeenCalled();

      act(() => renderer?.unmount());
      renderer = null;
      switchServerEvents(false);
      switchServerEvents(true);
      expect(client.connect).toHaveBeenCalledTimes(1);
    });
  });
});
