// Regression for issue #955: the SSE connection must be gated by its remote kill switch, so a
// reconnect storm can be stopped without an app release.

import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, create, type ReactTestRenderer } from 'react-test-renderer';

import { createMemoryFileStore } from '@shared/files/__tests__/memoryFileStore';
import { applyKillSwitches, setKillSwitchFileStore } from '@shared/killSwitch/killSwitch';

import {
  useServerEvents,
  type ServerEventsClient,
  type ServerEventsClientFactory,
} from '../useServerEvents';

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
            listeners.splice(listeners.indexOf(handler), 1);
          }),
        };
      }),
    },
    __listeners: listeners,
  };
});

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
