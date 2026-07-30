import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
// @ts-expect-error no type declarations shipped for react-test-renderer here
import { act, create, type ReactTestRenderer } from 'react-test-renderer';

import { useServerEvents } from '../useServerEvents';
import { applyServerEvent } from '../applyServerEvent';
import { supabase } from '@shared/auth/supabaseClient';

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

interface MockSSEClientInstance {
  url: string;
  getToken: () => Promise<string | null>;
  onEvent: (event: unknown) => void;
  onError: (error: unknown) => void;
  connect: jest.Mock;
  disconnect: jest.Mock;
  dispose: jest.Mock;
}

jest.mock('../sse-client', () => {
  const instances: MockSSEClientInstance[] = [];

  class MockSSEClient {
    url: string;
    getToken: () => Promise<string | null>;
    onEvent: (event: unknown) => void;
    onError: (error: unknown) => void;
    connect = jest.fn();
    disconnect = jest.fn();
    dispose = jest.fn();

    constructor(
      url: string,
      getToken: () => Promise<string | null>,
      onEvent: (event: unknown) => void,
      onError: (error: unknown) => void,
    ) {
      this.url = url;
      this.getToken = getToken;
      this.onEvent = onEvent;
      this.onError = onError;
      instances.push(this as unknown as MockSSEClientInstance);
    }
  }

  return { SSEClient: MockSSEClient, __instances: instances };
});

const { __instances: instances } = jest.requireMock('../sse-client') as {
  __instances: MockSSEClientInstance[];
};

function instanceAt(index: number): MockSSEClientInstance {
  const instance = instances[index];
  if (!instance) throw new Error(`no MockSSEClient instance at index ${index}`);
  return instance;
}

const { __listeners: appStateListeners } = jest.requireMock(
  'react-native/Libraries/AppState/AppState',
) as { __listeners: AppStateChangeHandler[] };

function emitAppStateChange(state: string): void {
  [...appStateListeners].forEach((handler) => handler(state));
}

function Harness(): null {
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
    const event = { id: '1', type: 'resync', data: {} };

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
});
