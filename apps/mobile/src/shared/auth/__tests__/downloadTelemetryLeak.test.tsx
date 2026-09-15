import React from 'react';
import type { Session } from '@supabase/supabase-js';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, act, waitFor } from '@testing-library/react-native';

import { startDownload, useDownloadStore } from '@shared/acquisition/downloadStore';
import { enqueueCritical, flushOutbox, _resetOutboxForTest } from '@shared/telemetry/outbox';

import { useSession } from '../useSession';
import { supabase } from '../supabaseClient';

const { __http } = require('../../../../jest/doubles/fetch.js');

type AuthCallback = (event: string, session: Session | null) => void;
type Deferred = { promise: Promise<void>; resolve: () => void };

const USER_A = { id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa' } as Session['user'];
const USER_B = { id: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb' } as Session['user'];

const EVENTS = 'POST /v1/discovery/events';
const OUTBOX_URI = 'file:///document/telemetry/critical-outbox.json';

function sessionFor(user: Session['user'], token: string): Session {
  return {
    access_token: token,
    refresh_token: `${token}-refresh`,
    expires_at: Math.floor(Date.now() / 1000) + 3600,
    expires_in: 3600,
    token_type: 'bearer',
    user,
  } as Session;
}

function makeWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

function deferred(): Deferred {
  let resolve!: () => void;
  const promise = new Promise<void>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

let authCallbacks: AuthCallback[] = [];

function mockSessionAs(session: Session | null): void {
  jest
    .spyOn(supabase.auth, 'getSession')
    .mockResolvedValue({ data: { session }, error: null } as never);
}

function bootWith(initial: Session | null) {
  mockSessionAs(initial);
  jest.spyOn(supabase.auth, 'onAuthStateChange').mockImplementation(((cb: AuthCallback) => {
    authCallbacks.push(cb);
    return { data: { subscription: { unsubscribe: jest.fn() } } };
  }) as never);
  return renderHook(() => useSession(), { wrapper: makeWrapper(new QueryClient()) });
}

function switchAccountTo(session: Session): void {
  mockSessionAs(session);
  act(() => {
    authCallbacks.forEach((cb) => cb('SIGNED_IN', session));
  });
}

type SentEvent = { token: string | undefined; body: Record<string, unknown> };

function sentEvents(): SentEvent[] {
  return (
    __http.requests as {
      method: string;
      path: string;
      headers: Record<string, string>;
      body: unknown;
    }[]
  )
    .filter((r) => `${r.method} ${r.path}` === EVENTS)
    .map((r) => ({
      token: r.headers['Authorization']?.replace('Bearer ', ''),
      body: JSON.parse(String(r.body)) as Record<string, unknown>,
    }));
}

function fsDouble(): {
  seedFile(uri: string, c: string): void;
  readFile(uri: string): string | undefined;
} {
  return (require('expo-file-system') as { __fs: never }).__fs;
}

beforeEach(() => {
  authCallbacks = [];
  jest.restoreAllMocks();
  _resetOutboxForTest();
  act(() => {
    useDownloadStore.getState().reset();
  });
});

describe('account switch clears acquisition and telemetry state (#960)', () => {
  it("drops A's in-progress download and A's in-flight critical telemetry before B's session", async () => {
    const session = bootWith(sessionFor(USER_A, 'token-a'));
    await waitFor(() => expect(session.result.current.status).toBe('signed-in'));

    // A starts an acquisition: the downloading banner would show A's track.
    act(() => {
      startDownload('track-a', {
        title: 'A Song',
        artist: 'A Artist',
        artworkUrl: 'https://a/art',
      });
    });
    expect(useDownloadStore.getState().entries['track-a']).toBeDefined();

    // A queues two label-critical events while offline.
    __http.fail(EVENTS);
    await act(async () => {
      await enqueueCritical({ type: 'library_add', payload: { track_id: 'a-first' } });
      await enqueueCritical({ type: 'library_add', payload: { track_id: 'a-second' } });
    });

    // Network returns; a foreground flush starts sending A's queue and the first
    // request is still in flight when A signs out and B signs in.
    __http.reset();
    __http.reply(EVENTS, { status: 202 });
    const gate = deferred();
    const realFetch = global.fetch;
    const fetchSpy = jest
      .spyOn(global, 'fetch')
      .mockImplementationOnce(async (...args: Parameters<typeof fetch>) => {
        await gate.promise;
        return realFetch(...args);
      });
    let flushing!: Promise<void>;
    act(() => {
      flushing = flushOutbox();
    });
    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1));

    switchAccountTo(sessionFor(USER_B, 'token-b'));

    await act(async () => {
      gate.resolve();
      await flushing;
      await flushOutbox();
    });

    // Neither A's download nor any of A's telemetry survives into B's session.
    expect(useDownloadStore.getState().entries).toEqual({});
    expect(sentEvents().filter((e) => e.token === 'token-b')).toEqual([]);
    expect(fsDouble().readFile(OUTBOX_URI)).toBeUndefined();

    session.unmount();
  });

  it('a persisted outbox entry A left on disk is never sent when B is the account restored at launch', async () => {
    fsDouble().seedFile(
      OUTBOX_URI,
      JSON.stringify([
        {
          type: 'library_add',
          event_id: '11111111-1111-4111-8111-111111111111',
          client_occurred_at: '2026-09-01T00:00:00.000Z',
          owner_user_id: USER_A.id,
          payload: { track_id: 'a-track' },
        },
        {
          type: 'library_add',
          event_id: '22222222-2222-4222-8222-222222222222',
          client_occurred_at: '2026-09-01T00:00:01.000Z',
          owner_user_id: USER_B.id,
          payload: { track_id: 'b-track' },
        },
      ]),
    );
    // Simulate a cold start: the outbox has not yet restored from disk.
    _resetOutboxForTest({ restored: false });

    __http.reply(EVENTS, { status: 202 });
    const session = bootWith(sessionFor(USER_B, 'token-b'));
    await waitFor(() => expect(session.result.current.status).toBe('signed-in'));

    await act(async () => {
      await flushOutbox();
    });

    const sent = sentEvents();
    expect(sent.map((e) => (e.body['payload'] as Record<string, unknown>)['track_id'])).toEqual([
      'b-track',
    ]);
    // The owner tag is local bookkeeping, never sent over the wire.
    expect(sent[0]?.body).not.toHaveProperty('owner_user_id');

    session.unmount();
  });

  it('an entry B enqueues is tagged with B and still delivered under B', async () => {
    __http.reply(EVENTS, { status: 202 });
    const session = bootWith(sessionFor(USER_B, 'token-b'));
    await waitFor(() => expect(session.result.current.status).toBe('signed-in'));

    __http.reset();
    __http.fail(EVENTS);
    await act(async () => {
      await enqueueCritical({ type: 'wrong_album', payload: { title: 'T', artist: 'A' } });
    });
    const persisted = JSON.parse(fsDouble().readFile(OUTBOX_URI) ?? '[]') as Record<
      string,
      unknown
    >[];
    expect(persisted.map((e) => e['owner_user_id'])).toEqual([USER_B.id]);

    __http.reset();
    __http.reply(EVENTS, { status: 202 });
    await act(async () => {
      await flushOutbox();
    });
    expect(sentEvents().map((e) => e.token)).toEqual(['token-b']);

    session.unmount();
  });
});
