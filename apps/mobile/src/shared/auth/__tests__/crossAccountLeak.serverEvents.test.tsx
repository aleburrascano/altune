import React from 'react';
import type { Session } from '@supabase/supabase-js';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, act, waitFor } from '@testing-library/react-native';

import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import type { ServerEvent } from '@shared/events/sse-client';
import {
  useServerEvents,
  type ServerEventsClient,
  type ServerEventsClientFactory,
} from '@shared/events/useServerEvents';

import { useSession } from '../useSession';
import { supabase } from '../supabaseClient';

type AuthCallback = (event: string, session: Session | null) => void;

// The leak this file guards needs the app foregrounded across the switch: that is when the
// stream opened for the previous account is still live (#1772).
jest.mock('react-native/Libraries/AppState/AppState', () => ({
  default: {
    currentState: 'active',
    isAvailable: true,
    addEventListener: jest.fn(() => ({ remove: jest.fn() })),
  },
}));

const USER_A = { id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa' } as Session['user'];
const USER_B = { id: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb' } as Session['user'];

function sessionFor(user: Session['user']): Session {
  return {
    access_token: `token-${user.id}`,
    refresh_token: `refresh-${user.id}`,
    expires_at: Math.floor(Date.now() / 1000) + 3600,
    expires_in: 3600,
    token_type: 'bearer',
    user,
  } as Session;
}

function acquisitionStartedFor(trackId: string): ServerEvent {
  return { id: `evt-${trackId}`, type: 'track_acquisition_started', data: { track_id: trackId } };
}

/**
 * A stream the test can push events down. Delivery follows SSEClient's own rules: only while a
 * connection is open, and never again once disposed, since disposal is terminal there.
 */
interface FakeStream extends ServerEventsClient {
  deliver: (event: ServerEvent) => void;
}

let streams: FakeStream[] = [];

const openFakeStream: ServerEventsClientFactory = (_url, _getToken, onEvent) => {
  let isOpen = false;
  let isDisposed = false;
  const stream: FakeStream = {
    deliver: (event) => {
      if (isOpen) onEvent(event);
    },
    connect: async () => {
      isOpen = !isDisposed;
    },
    disconnect: () => {
      isOpen = false;
    },
    dispose: () => {
      isOpen = false;
      isDisposed = true;
    },
  };
  streams.push(stream);
  return stream;
};

function streamAt(index: number): FakeStream {
  const stream = streams[index];
  if (!stream) throw new Error(`no SSE stream was opened at index ${index}`);
  return stream;
}

function deliverOn(index: number, event: ServerEvent): void {
  act(() => {
    streamAt(index).deliver(event);
  });
}

function makeWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

let authCallbacks: AuthCallback[] = [];

function bootSignedInAs(user: Session['user']): void {
  jest
    .spyOn(supabase.auth, 'getSession')
    .mockResolvedValue({ data: { session: sessionFor(user) }, error: null } as never);
  jest.spyOn(supabase.auth, 'onAuthStateChange').mockImplementation(((cb: AuthCallback) => {
    authCallbacks.push(cb);
    return { data: { subscription: { unsubscribe: jest.fn() } } };
  }) as never);
}

function emitAuth(event: string, session: Session | null): void {
  act(() => {
    authCallbacks.forEach((cb) => cb(event, session));
  });
}

function statusesInCache(): Record<string, unknown> {
  return useTrackStatusStore.getState().statuses;
}

/** The app root's pairing: the SSE connection runs beside the session, not under it. */
async function renderAppSignedInAs(user: Session['user']) {
  bootSignedInAs(user);
  const rendered = renderHook(
    () => {
      useSession();
      useServerEvents(openFakeStream);
    },
    { wrapper: makeWrapper(new QueryClient()) },
  );
  await waitFor(() => expect(streams).toHaveLength(1));
  return rendered;
}

beforeEach(() => {
  authCallbacks = [];
  streams = [];
  jest.restoreAllMocks();
  act(() => {
    useTrackStatusStore.getState().reset();
  });
});

describe("a server event on the previous account's stream does not reach the next one (#1772)", () => {
  it.each([
    ['user A signs out', 'SIGNED_OUT', null],
    ['the device switches straight to user B', 'SIGNED_IN', sessionFor(USER_B)],
  ] as const)(
    '%s -> an event still arriving for A no longer patches the shared cache',
    async (_label, event, next) => {
      const rendered = await renderAppSignedInAs(USER_A);
      deliverOn(0, acquisitionStartedFor('track-of-user-a'));
      expect(statusesInCache()).toHaveProperty('track-of-user-a');

      emitAuth(event, next);
      deliverOn(0, acquisitionStartedFor('other-track-of-user-a'));

      expect(statusesInCache()).toEqual({});
      rendered.unmount();
    },
  );

  it('opens a fresh stream for the next account, so B still receives their own events', async () => {
    const rendered = await renderAppSignedInAs(USER_A);

    emitAuth('SIGNED_IN', sessionFor(USER_B));

    expect(streams).toHaveLength(2);
    deliverOn(1, acquisitionStartedFor('track-of-user-b'));
    expect(statusesInCache()).toHaveProperty('track-of-user-b');
    rendered.unmount();
  });

  it('keeps the stream open across a token refresh for the same user', async () => {
    const rendered = await renderAppSignedInAs(USER_A);

    emitAuth('TOKEN_REFRESHED', sessionFor(USER_A));

    expect(streams).toHaveLength(1);
    deliverOn(0, acquisitionStartedFor('track-of-user-a'));
    expect(statusesInCache()).toHaveProperty('track-of-user-a');
    rendered.unmount();
  });
});
