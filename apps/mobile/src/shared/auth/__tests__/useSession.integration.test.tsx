import React, { type ReactNode } from 'react';
import type { Session } from '@supabase/supabase-js';
import * as ReactQuery from '@tanstack/react-query';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, render, act, waitFor, screen } from '@testing-library/react-native';

import {
  linkTrackIdentity,
  patchTrackStatus,
  trackIdentityKey,
  useTrackStatusStore,
} from '@shared/acquisition/trackStatusStore';
import { enqueueCritical, flushOutbox, _resetOutboxForTest } from '@shared/telemetry/outbox';
import { useOwnedTrack } from '@features/detail/hooks/useOwnedTrack';
import type { TrackExtras } from '@features/detail/extras-accessors';
import { TrackSaveControl } from '@features/detail/ui/TrackSaveControl';
import type { ServerEvent } from '@shared/events/sse-client';
import {
  useServerEvents,
  type ServerEventsClient,
  type ServerEventsClientFactory,
} from '@shared/events/useServerEvents';
import { startDownload, useDownloadStore } from '@shared/acquisition/downloadStore';
import { getSearchState, setSearchState } from '@features/discover/search-state';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import { clearDetailHandoffs, detailHref, readDetailHandoff } from '@shared/lib/detail-handoff';
import type * as PinnedStoreModule from '@shared/offline/pinnedStore';

import { useSession } from '../useSession';
import { useSignOut } from '../useSignOut';
import { supabase } from '../supabaseClient';
import type * as SupabaseClientModule from '../supabaseClient';
import type * as UseSessionModule from '../useSession';
import { asTrackId } from '@shared/api-client/ids';

// The server-event tests need the app foregrounded across the switch: that is when the
// stream opened for the previous account is still live.
jest.mock('react-native/Libraries/AppState/AppState', () => ({
  default: {
    currentState: 'active',
    isAvailable: true,
    addEventListener: jest.fn(() => ({ remove: jest.fn() })),
  },
}));

jest.mock('@shared/api-client/audio', () => ({ fetchAudioUrls: jest.fn() }));

const { __http } = require('../../../../jest/doubles/fetch.js');

type AuthCallback = (event: string, session: Session | null) => void;

const USER_A = { id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa' } as Session['user'];
const USER_B = { id: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb' } as Session['user'];

const EVENTS = 'POST /v1/discovery/events';

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

function stampedElsewhere(): TrackExtras {
  return { trackId: null, acquisitionStatus: null } as unknown as TrackExtras;
}

function makeWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

let authCallbacks: AuthCallback[] = [];

function bootSignedIn(initial: Session): void {
  jest
    .spyOn(supabase.auth, 'getSession')
    .mockResolvedValue({ data: { session: initial }, error: null } as never);
  jest.spyOn(supabase.auth, 'onAuthStateChange').mockImplementation(((cb: AuthCallback) => {
    authCallbacks.push(cb);
    return { data: { subscription: { unsubscribe: jest.fn() } } };
  }) as never);
}

function switchAccountTo(session: Session): void {
  jest
    .spyOn(supabase.auth, 'getSession')
    .mockResolvedValue({ data: { session }, error: null } as never);
  act(() => {
    authCallbacks.forEach((cb) => cb('SIGNED_IN', session));
  });
}

beforeEach(() => {
  authCallbacks = [];
  jest.restoreAllMocks();
  _resetOutboxForTest();
  act(() => {
    useTrackStatusStore.getState().reset();
  });
});

// Disarm the outbox retry timer a failed send arms, so it cannot fire after the test.
afterEach(() => _resetOutboxForTest());

describe('cross-account acquisition-status leak on a shared device (#676)', () => {
  it('after A saves a track and B signs in, useOwnedTrack no longer resolves A save status for a shared identity', async () => {
    bootSignedIn(sessionFor(USER_A, 'token-a'));
    const queryClient = new QueryClient();
    const session = renderHook(() => useSession(), { wrapper: makeWrapper(queryClient) });
    await waitFor(() => expect(session.result.current.status).toBe('signed-in'));

    // User A saves a track: the identity store links the normalized key to A's trackId.
    const identity = trackIdentityKey('Song Title', 'The Artist');
    patchTrackStatus(asTrackId('track-owned-by-a'), { acquisitionStatus: 'ready', failureMessage: null });
    linkTrackIdentity(identity, asTrackId('track-owned-by-a'));

    const owned = renderHook(() =>
      useOwnedTrack(stampedElsewhere(), { title: 'Song Title', artist: 'The Artist' }),
    );
    // Sanity: while A is signed in, the shared identity resolves to A's saved track.
    expect(owned.result.current).toEqual({
      trackId: 'track-owned-by-a',
      acquisitionStatus: 'ready',
    });

    // A signs out; B signs in on the same device.
    switchAccountTo(sessionFor(USER_B, 'token-b'));

    expect(owned.result.current).toBeNull();
    expect(useTrackStatusStore.getState().statuses).toEqual({});
    expect(useTrackStatusStore.getState().identities).toEqual({});

    session.unmount();
  });

  it("TrackSaveControl stops showing A's 'in library' state and reverts to a savable control once B signs in", async () => {
    bootSignedIn(sessionFor(USER_A, 'token-a'));
    const queryClient = new QueryClient();
    const session = renderHook(() => useSession(), { wrapper: makeWrapper(queryClient) });
    await waitFor(() => expect(session.result.current.status).toBe('signed-in'));

    patchTrackStatus(asTrackId('track-owned-by-a'), { acquisitionStatus: 'ready', failureMessage: null });
    linkTrackIdentity(trackIdentityKey('Song Title', 'The Artist'), asTrackId('track-owned-by-a'));

    render(
      <TrackSaveControl
        owned={null}
        onPress={jest.fn()}
        title="Song Title"
        artist="The Artist"
      />,
    );
    // Before the switch, the control reflects A's saved track for B's shared identity.
    expect(screen.getByLabelText('Song Title in library')).toBeTruthy();

    switchAccountTo(sessionFor(USER_B, 'token-b'));

    expect(screen.queryByLabelText('Song Title in library')).toBeNull();
    expect(screen.getByLabelText('Save Song Title')).toBeTruthy();

    session.unmount();
  });
});

describe('cross-account queued-telemetry leak on a shared device (#676)', () => {
  it("a critical entry A queued while offline is dropped on account switch, never sent under B's session", async () => {
    bootSignedIn(sessionFor(USER_A, 'token-a'));
    const queryClient = new QueryClient();
    const session = renderHook(() => useSession(), { wrapper: makeWrapper(queryClient) });
    await waitFor(() => expect(session.result.current.status).toBe('signed-in'));

    // A files a wrong_album report while the network is down: it lands in the outbox.
    __http.fail(EVENTS);
    await act(async () => {
      await enqueueCritical({ type: 'wrong_album', payload: { title: 'Song', artist: 'Artist' } });
    });
    const attemptsAsA = __http.countFor(EVENTS);
    expect(attemptsAsA).toBeGreaterThanOrEqual(1);

    // A signs out; B signs in on the same device.
    switchAccountTo(sessionFor(USER_B, 'token-b'));

    // The network is back; a foreground flush would send anything still queued.
    __http.reply(EVENTS, { status: 202 });
    await act(async () => {
      await flushOutbox();
    });

    // Nothing new was sent — A's report was dropped, not misattributed to B.
    expect(__http.countFor(EVENTS)).toBe(attemptsAsA);

    session.unmount();
  });
});

describe('session restore across process death', () => {
  describe('AC#4 — a session survives process death (in-process half: real storage adapter, module registry discarded, session restored)', () => {
    const FIXTURE_USER_ID = '11111111-1111-4111-8111-111111111111';

    function seedSession(): {
      access_token: string;
      refresh_token: string;
      expires_at: number;
      expires_in: number;
      token_type: string;
      user: { id: string };
    } {
      return {
        access_token: 'fixture-access-token',
        refresh_token: 'fixture-refresh-token',
        expires_at: Math.floor(Date.now() / 1000) + 3600,
        expires_in: 3600,
        token_type: 'bearer',
        user: { id: FIXTURE_USER_ID },
      };
    }

    beforeEach(() => {
      const { __secureStore } = require('expo-secure-store');
      __secureStore.reset();
      delete require.cache[require.resolve('../supabaseClient')];
    });

    it('restores the previously written session after the app "restarts" — no re-authentication is asked of the user', async () => {
      const first = require('../supabaseClient');
      await first.supabase.auth.getSession();
      await first.supabase.auth._saveSession(seedSession());
      await first.supabase.auth.stopAutoRefresh();

      delete require.cache[require.resolve('../supabaseClient')];

      const { useSession } = require('../useSession');
      const queryClient = new QueryClient();
      const { result, unmount } = renderHook(() => useSession(), {
        wrapper: makeWrapper(queryClient),
      });

      expect(result.current.status).toBe('loading');

      await waitFor(() => {
        expect(result.current.status).toBe('signed-in');
      });

      expect(result.current).toMatchObject({
        status: 'signed-in',
        session: { user: { id: FIXTURE_USER_ID } },
      });

      unmount();
      const second = require('../supabaseClient');
      await second.supabase.auth.stopAutoRefresh();
    });

    it('a device with no previously written session comes back signed-out, not stuck loading', async () => {
      const { useSession } = require('../useSession');
      const queryClient = new QueryClient();
      const { result, unmount } = renderHook(() => useSession(), {
        wrapper: makeWrapper(queryClient),
      });

      await waitFor(() => {
        expect(result.current.status).toBe('signed-out');
      });

      unmount();
      const client = require('../supabaseClient');
      await client.supabase.auth.stopAutoRefresh();
    });
  });
});

describe('server event stream across an account switch', () => {
  type AuthCallback = (event: string, session: Session | null) => void;

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
});

describe('acquisition and telemetry state across an account switch', () => {
  type AuthCallback = (event: string, session: Session | null) => void;
  type Deferred = { promise: Promise<void>; resolve: () => void };
  const OUTBOX_URI = 'file:///document/telemetry/critical-outbox.json';

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

  // Disarm the outbox retry timer a failed send arms, so it cannot fire after the test.
  afterEach(() => _resetOutboxForTest());

  describe('account switch clears acquisition and telemetry state (#960)', () => {
    it("drops A's in-progress download and A's in-flight critical telemetry before B's session", async () => {
      const session = bootWith(sessionFor(USER_A, 'token-a'));
      await waitFor(() => expect(session.result.current.status).toBe('signed-in'));

      // A starts an acquisition: the downloading banner would show A's track.
      act(() => {
        startDownload(asTrackId('track-a'), {
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
      const persisted = (
        JSON.parse(fsDouble().readFile(OUTBOX_URI) ?? '{"entries":[]}') as {
          entries: Record<string, unknown>[];
        }
      ).entries;
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
});

describe('search state across an identity change', () => {
  type AuthCallback = (event: string, session: Session | null) => void;

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

  const TAPPED: DiscoveryResult = {
    kind: 'track',
    title: 'Anti-Hero',
    subtitle: 'Taylor Swift',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: {},
  };

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

  let tappedHandoffId = '';

  function userASearchedAndTapped(): void {
    setSearchState('taylor swift', 'taylor swift');
    tappedHandoffId = detailHref('/discover/detail', TAPPED, 'search-of-user-a').params.handoff;
  }

  function expectNothingOfUserALeft(): void {
    expect(getSearchState()).toEqual({ query: '', inputValue: '' });
    expect(readDetailHandoff(tappedHandoffId)).toBeNull();
  }

  beforeEach(() => {
    authCallbacks = [];
    jest.restoreAllMocks();
    setSearchState('', '');
    clearDetailHandoffs();
  });

  describe('search text and last-tapped result do not survive an identity change (#772)', () => {
    it.each([
      ['user A signs out', 'SIGNED_OUT', null],
      ['the device switches straight to user B', 'SIGNED_IN', sessionFor(USER_B)],
    ] as const)('%s -> search-state and detail-handoff are cleared', async (_label, event, next) => {
      bootSignedInAs(USER_A);
      const { result } = renderHook(() => useSession(), {
        wrapper: makeWrapper(new QueryClient()),
      });
      await waitFor(() => expect(result.current.status).toBe('signed-in'));

      userASearchedAndTapped();
      emitAuth(event, next);

      expectNothingOfUserALeft();
    });

    it('a token refresh for the same user keeps the search the user is in the middle of', async () => {
      bootSignedInAs(USER_A);
      const { result } = renderHook(() => useSession(), {
        wrapper: makeWrapper(new QueryClient()),
      });
      await waitFor(() => expect(result.current.status).toBe('signed-in'));

      userASearchedAndTapped();
      emitAuth('TOKEN_REFRESHED', sessionFor(USER_A));

      expect(getSearchState()).toEqual({ query: 'taylor swift', inputValue: 'taylor swift' });
      expect(readDetailHandoff(tappedHandoffId)).toEqual({
        result: TAPPED,
        searchId: 'search-of-user-a',
      });
    });

    it.each([
      ['succeeds', () => ({ error: null })],
      ['returns an error', () => ({ error: { message: 'network', status: 0 } })],
      [
        'throws',
        () => {
          throw new Error('network request failed');
        },
      ],
    ] as const)(
      'useSignOut clears both even when supabase signOut %s and no auth event arrives',
      async (_label, outcome) => {
        jest.spyOn(supabase.auth, 'signOut').mockImplementation((async () => outcome()) as never);
        const { result } = renderHook(() => useSignOut(), {
          wrapper: makeWrapper(new QueryClient()),
        });

        userASearchedAndTapped();
        await act(async () => {
          await result.current.signOut();
        });

        expectNothingOfUserALeft();
      },
    );
  });
});

describe('pinned downloads across a killed sign-out', () => {
  // #835: pinned downloads are keyed by trackId only. When the app dies before the
  // sign-out cleanup finishes, pinned.json and the audio survive, and the next
  // account to sign in on the device must not be able to see or play them.

  type FsDouble = {
    seedFile(uri: string, contents: string): void;
    readFile(uri: string): string | undefined;
    allFiles(): Record<string, string>;
  };

  type App = {
    session: typeof UseSessionModule;
    supabase: (typeof SupabaseClientModule)['supabase'];
    pinned: typeof PinnedStoreModule;
    audio: { fetchAudioUrls: jest.Mock };
  };

  const USER_A = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa';
  const USER_B = 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb';
  const AUDIO_URI = 'file:///document/offline-audio/t1.mp3';
  const INDEX_URI = 'file:///document/offline/pinned.json';

  function currentFs(): FsDouble {
    return (require('expo-file-system') as { __fs: FsDouble }).__fs;
  }

  function sessionFor(userId: string): Session {
    return {
      access_token: `token-${userId}`,
      refresh_token: `refresh-${userId}`,
      expires_in: 3600,
      token_type: 'bearer',
      user: { id: userId },
    } as unknown as Session;
  }

  // A relaunch gets fresh app modules, but React and react-query stay the
  // renderer's instances so hooks from the fresh modules still render.
  function bootApp(): App {
    jest.doMock('react', () => React);
    jest.doMock('@tanstack/react-query', () => ReactQuery);
    return {
      session: require('../useSession'),
      supabase: (require('../supabaseClient') as typeof SupabaseClientModule).supabase,
      pinned: require('@shared/offline/pinnedStore'),
      audio: require('@shared/api-client/audio'),
    };
  }

  // Process death: memory is gone, only what was written to disk survives.
  function killAndRelaunch(): App {
    const snapshot = currentFs().allFiles();
    jest.resetModules();
    const fs = currentFs();
    for (const [uri, contents] of Object.entries(snapshot)) fs.seedFile(uri, contents);
    return bootApp();
  }

  async function signIn(app: App, userId: string): Promise<() => void> {
    jest
      .spyOn(app.supabase.auth, 'getSession')
      .mockResolvedValue({ data: { session: sessionFor(userId) }, error: null } as never);
    jest
      .spyOn(app.supabase.auth, 'onAuthStateChange')
      .mockReturnValue({ data: { subscription: { unsubscribe: jest.fn() } } } as never);
    const client = new ReactQuery.QueryClient();
    const wrapper = ({ children }: { children: ReactNode }) =>
      React.createElement(ReactQuery.QueryClientProvider, { client }, children);
    const hook = renderHook(() => app.session.useSession(), { wrapper });
    await waitFor(() => expect(hook.result.current.status).toBe('signed-in'));
    return hook.unmount;
  }

  async function pinAsReady(app: App, trackId: string): Promise<void> {
    app.audio.fetchAudioUrls.mockResolvedValue([
      { trackId, url: `https://cdn.example.com/${trackId}.mp3`, version: 'v1' },
    ]);
    app.pinned.usePinnedStore.getState().pin(asTrackId(trackId));
    await waitFor(() =>
      expect(app.pinned.usePinnedStore.getState().entries[trackId]?.status).toBe('ready'),
    );
  }

  beforeEach(() => {
    jest.resetModules();
  });

  describe('pinned downloads never cross accounts after a killed sign-out (#835)', () => {
    it('B signing in after A was killed mid sign-out sees and plays none of A downloads', async () => {
      const first = bootApp();
      const unmountA = await signIn(first, USER_A);
      await pinAsReady(first, 't1');
      unmountA();
      // A's sign-out resolved but the app died before any cleanup ran.
      expect(currentFs().readFile(AUDIO_URI)).toBeDefined();

      const second = killAndRelaunch();
      // What a fresh launch reads before anyone signs in is still A's index.
      expect(second.pinned.usePinnedStore.getState().entries['t1']).toBeDefined();

      const unmountB = await signIn(second, USER_B);

      expect(second.pinned.usePinnedStore.getState().entries).toEqual({});
      expect(second.pinned.resolvePinnedUri(asTrackId('t1'))).toBeUndefined();
      expect(second.pinned.pinnedByteTotal()).toBe(0);
      expect(currentFs().allFiles()[AUDIO_URI]).toBeUndefined();
      expect(JSON.parse(currentFs().readFile(INDEX_URI) ?? 'null')).toEqual({ schemaVersion: 1, entries: {} });
      unmountB();

      // And B's own relaunch does not inherit A's downloads back from disk.
      const third = killAndRelaunch();
      expect(third.pinned.usePinnedStore.getState().entries).toEqual({});
    });

    it('A relaunching into their own session keeps their downloads', async () => {
      const first = bootApp();
      const unmountA = await signIn(first, USER_A);
      await pinAsReady(first, 't1');
      unmountA();

      const second = killAndRelaunch();
      const unmountAgain = await signIn(second, USER_A);

      expect(second.pinned.resolvePinnedUri(asTrackId('t1'))).toBe(AUDIO_URI);
      expect(currentFs().readFile(AUDIO_URI)).toBeDefined();
      unmountAgain();
    });

    it('downloads of unknown owner (written before ownership was recorded) are cleared, not adopted', async () => {
      const fs = currentFs();
      fs.seedFile(AUDIO_URI, 'audio-bytes');
      fs.seedFile(
        INDEX_URI,
        JSON.stringify({ t1: { trackId: 't1', status: 'ready', uri: AUDIO_URI } }),
      );
      const app = killAndRelaunch();

      const unmount = await signIn(app, USER_B);

      expect(app.pinned.resolvePinnedUri(asTrackId('t1'))).toBeUndefined();
      expect(currentFs().allFiles()[AUDIO_URI]).toBeUndefined();
      unmount();
    });
  });
});
