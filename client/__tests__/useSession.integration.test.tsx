import React, { useEffect } from 'react';
import type { Session } from '@supabase/supabase-js';
import { QueryClient, QueryClientProvider, useQueryClient } from '@tanstack/react-query';
import { renderHook, render, act, waitFor, screen } from '@testing-library/react-native';

import {
  linkTrackIdentity,
  patchTrackStatus,
  startDownload,
  trackIdentityKey,
  useDownloadStore,
  useTrackStatusStore,
} from '@shared/acquisition';
import { installAuthClient, useSession, useSignOut } from '@shared/auth';
import type { DiscoveryResult } from '@shared/catalog';
import { asTrackId } from '@shared/catalog';
import {
  startServerEvents,
  type ServerEvent,
  type ServerEventsClientFactory,
} from '@shared/events';
import { detailHref, readDetailHandoff } from '@shared/navigation';
import type * as OfflineDoor from '@shared/offline';
import type * as AuthDoor from '@shared/auth';
import type * as SessionDoor from '@shared/session';
import { runSignOutCleanups } from '@shared/session';
import {
  createAppTelemetry,
  enqueueCritical,
  flushOutbox,
  followSignedInUser,
  getSessionId,
  installTelemetry,
} from '@shared/telemetry';
import { TrackSaveControl, useResolvedOwnedTrack } from '@features/detail';
import {
  getSearchState,
  registerSearchStateSignOutReset,
  setSearchState,
} from '@features/discovery';
import { appAuthParts, createAuthClient } from '@/mobile/authClient';
import { appAuthConfig } from '@testing/appAuthConfig';
import { scriptedAuth, type ScriptedAuth, type SignOutOutcome } from '@testing/scriptedAuth';
import { EVENT_ROUTES } from '@testing/eventRoutes';

const { __http } = require('@testing/doubles/fetch.js');
const secureStore = require('expo-secure-store') as typeof import('expo-secure-store') & {
  __secureStore: { reset(): void };
};
const { __fs: outboxFs } = require('expo-file-system') as { __fs: never };

const USER_A = { id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa' } as Session['user'];
const USER_B = { id: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb' } as Session['user'];

const EVENTS = EVENT_ROUTES;

function sessionFor(user: Session['user'], token: string = `token-${user.id}`): Session {
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

function bootSignedIn(initial: Session | null, signOut?: SignOutOutcome): ScriptedAuth {
  return scriptedAuth(installAuthClient, initial, signOut);
}

function startOutboxOverDevice(): void {
  installTelemetry(createAppTelemetry());
  followSignedInUser();
}

beforeEach(() => {
  startOutboxOverDevice();
  act(() => {
    useTrackStatusStore.getState().reset();
  });
});

describe('cross-account acquisition-status leak on a shared device', () => {
  it('after A saves a track and B signs in, useOwnedTrack no longer resolves A save status for a shared identity', async () => {
    const auth = bootSignedIn(sessionFor(USER_A, 'token-a'));
    const queryClient = new QueryClient();
    const session = renderHook(() => useSession(), { wrapper: makeWrapper(queryClient) });
    await waitFor(() => expect(session.result.current.status).toBe('signed-in'));

    const identity = trackIdentityKey('Song Title', 'The Artist');
    patchTrackStatus(asTrackId('track-owned-by-a'), {
      acquisitionStatus: 'ready',
      failureMessage: null,
    });
    linkTrackIdentity(identity, asTrackId('track-owned-by-a'));

    const owned = renderHook(() =>
      useResolvedOwnedTrack(null, { title: 'Song Title', artist: 'The Artist' }),
    );
    expect(owned.result.current).toEqual({
      trackId: 'track-owned-by-a',
      acquisitionStatus: 'ready',
    });

    auth.switchTo(sessionFor(USER_B, 'token-b'));

    expect(owned.result.current).toBeNull();
    expect(useTrackStatusStore.getState().statuses).toEqual({});
    expect(useTrackStatusStore.getState().identities).toEqual({});

    session.unmount();
  });

  it("TrackSaveControl stops showing A's 'in library' state and reverts to a savable control once B signs in", async () => {
    const auth = bootSignedIn(sessionFor(USER_A, 'token-a'));
    const queryClient = new QueryClient();
    const session = renderHook(() => useSession(), { wrapper: makeWrapper(queryClient) });
    await waitFor(() => expect(session.result.current.status).toBe('signed-in'));

    patchTrackStatus(asTrackId('track-owned-by-a'), {
      acquisitionStatus: 'ready',
      failureMessage: null,
    });
    linkTrackIdentity(trackIdentityKey('Song Title', 'The Artist'), asTrackId('track-owned-by-a'));

    render(
      <TrackSaveControl
        owned={null}
        onPress={() => undefined}
        title="Song Title"
        artist="The Artist"
      />,
    );
    expect(screen.getByLabelText('Song Title in library')).toBeTruthy();

    auth.switchTo(sessionFor(USER_B, 'token-b'));

    expect(screen.queryByLabelText('Song Title in library')).toBeNull();
    expect(screen.getByLabelText('Save Song Title')).toBeTruthy();

    session.unmount();
  });
});

describe('cross-account queued-telemetry leak on a shared device', () => {
  it("a critical entry A queued while offline is dropped on account switch, never sent under B's session", async () => {
    const auth = bootSignedIn(sessionFor(USER_A, 'token-a'));
    const queryClient = new QueryClient();
    const session = renderHook(() => useSession(), { wrapper: makeWrapper(queryClient) });
    await waitFor(() => expect(session.result.current.status).toBe('signed-in'));

    __http.fail(EVENTS);
    await act(async () => {
      await enqueueCritical({ type: 'wrong_album', payload: { title: 'Song', artist: 'Artist' } });
    });
    const attemptsAsA = __http.countFor(EVENTS);
    expect(attemptsAsA).toBeGreaterThanOrEqual(1);

    auth.switchTo(sessionFor(USER_B, 'token-b'));

    __http.reply(EVENTS, { status: 202 });
    await act(async () => {
      await flushOutbox();
    });

    expect(__http.countFor(EVENTS)).toBe(attemptsAsA);

    session.unmount();
  });
});

describe('telemetry session id across an account switch', () => {
  it("B's events do not carry A's session id when B signs in within the inactivity window", async () => {
    const auth = bootSignedIn(sessionFor(USER_A, 'token-a'));
    const queryClient = new QueryClient();
    const session = renderHook(() => useSession(), { wrapper: makeWrapper(queryClient) });
    await waitFor(() => expect(session.result.current.status).toBe('signed-in'));
    const idAsA = getSessionId();

    auth.switchTo(sessionFor(USER_B, 'token-b'));

    expect(getSessionId()).not.toBe(idAsA);

    session.unmount();
  });
});

describe('session restore across process death', () => {
  describe('a session survives process death (in-process half: real storage adapter, module registry discarded, session restored)', () => {
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
      secureStore.__secureStore.reset();
    });

    it('restores the previously written session after the app "restarts" — no re-authentication is asked of the user', async () => {
      await secureStore.setItemAsync('sb-fixture-auth-token', JSON.stringify(seedSession()));

      const restarted = createAuthClient(appAuthConfig(), appAuthParts());
      installAuthClient(restarted);

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
      await restarted.auth.stopAutoRefresh();
    });

    it('a device with no previously written session comes back signed-out, not stuck loading', async () => {
      const fresh = createAuthClient(appAuthConfig(), appAuthParts());
      installAuthClient(fresh);
      const queryClient = new QueryClient();
      const { result, unmount } = renderHook(() => useSession(), {
        wrapper: makeWrapper(queryClient),
      });

      await waitFor(() => {
        expect(result.current.status).toBe('signed-out');
      });
      unmount();
      await fresh.auth.stopAutoRefresh();
    });
  });
});

describe('server event stream across an account switch', () => {
  function acquisitionStartedFor(trackId: string): ServerEvent {
    return { id: `evt-${trackId}`, type: 'track_acquisition_started', data: { track_id: trackId } };
  }

  type FakeStream = ReturnType<ServerEventsClientFactory> & {
    deliver: (event: ServerEvent) => void;
  };

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

  function useFakeServerEvents(): void {
    const cache = useQueryClient();
    useEffect(
      () =>
        startServerEvents({
          cache,
          createClient: openFakeStream,
          getToken: async () => null,
          foreground: { isActive: () => true, subscribe: () => () => undefined },
          warn: () => undefined,
        }),
      [cache],
    );
  }

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

  let auth: ScriptedAuth;

  function statusesInCache(): Record<string, unknown> {
    return useTrackStatusStore.getState().statuses;
  }

  async function renderAppSignedInAs(user: Session['user']) {
    auth = bootSignedIn(sessionFor(user));
    const rendered = renderHook(
      () => {
        useSession();
        useFakeServerEvents();
      },
      { wrapper: makeWrapper(new QueryClient()) },
    );
    await waitFor(() => expect(streams).toHaveLength(1));
    return rendered;
  }

  beforeEach(() => {
    streams = [];
    act(() => {
      useTrackStatusStore.getState().reset();
    });
  });

  describe("a server event on the previous account's stream does not reach the next one", () => {
    it.each([
      ['user A signs out', 'SIGNED_OUT', null],
      ['the device switches straight to user B', 'SIGNED_IN', sessionFor(USER_B)],
    ] as const)(
      '%s -> an event still arriving for A no longer patches the shared cache',
      async (_label, event, next) => {
        const rendered = await renderAppSignedInAs(USER_A);
        deliverOn(0, acquisitionStartedFor('track-of-user-a'));
        expect(statusesInCache()).toHaveProperty('track-of-user-a');

        auth.emit(event, next);
        deliverOn(0, acquisitionStartedFor('other-track-of-user-a'));

        expect(statusesInCache()).toEqual({});
        rendered.unmount();
      },
    );

    it('opens a fresh stream for the next account, so B still receives their own events', async () => {
      const rendered = await renderAppSignedInAs(USER_A);

      auth.emit('SIGNED_IN', sessionFor(USER_B));

      expect(streams).toHaveLength(2);
      deliverOn(1, acquisitionStartedFor('track-of-user-b'));
      expect(statusesInCache()).toHaveProperty('track-of-user-b');
      rendered.unmount();
    });

    it('keeps the stream open across a token refresh for the same user', async () => {
      const rendered = await renderAppSignedInAs(USER_A);

      auth.emit('TOKEN_REFRESHED', sessionFor(USER_A));

      expect(streams).toHaveLength(1);
      deliverOn(0, acquisitionStartedFor('track-of-user-a'));
      expect(statusesInCache()).toHaveProperty('track-of-user-a');
      rendered.unmount();
    });
  });
});

describe('acquisition and telemetry state across an account switch', () => {
  const OUTBOX_URI = 'file:///document/telemetry/critical-outbox.json';

  let auth: ScriptedAuth;

  function bootWith(initial: Session | null) {
    auth = bootSignedIn(initial);
    return renderHook(() => useSession(), { wrapper: makeWrapper(new QueryClient()) });
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
      .filter((r) => EVENTS.includes(`${r.method} ${r.path}`))
      .map((r) => ({
        token: r.headers['Authorization']?.replace('Bearer ', ''),
        body: JSON.parse(String(r.body)) as Record<string, unknown>,
      }));
  }

  function fsDouble(): {
    seedFile(uri: string, c: string): void;
    readFile(uri: string): string | undefined;
  } {
    return outboxFs;
  }

  beforeEach(() => {
    act(() => {
      useDownloadStore.getState().reset();
    });
  });

  describe('account switch clears acquisition and telemetry state', () => {
    it("drops A's in-progress download and A's in-flight critical telemetry before B's session", async () => {
      const session = bootWith(sessionFor(USER_A, 'token-a'));
      await waitFor(() => expect(session.result.current.status).toBe('signed-in'));

      act(() => {
        startDownload(asTrackId('track-a'), {
          title: 'A Song',
          artist: 'A Artist',
          artworkUrl: 'https://a/art',
        });
      });
      expect(useDownloadStore.getState().entries['track-a']).toBeDefined();

      __http.fail(EVENTS);
      await act(async () => {
        await enqueueCritical({ type: 'library_add', payload: { track_id: 'a-first' } });
        await enqueueCritical({ type: 'library_add', payload: { track_id: 'a-second' } });
      });

      __http.reset();
      const release = __http.hold(EVENTS, { status: 202 });
      let flushing!: Promise<void>;
      act(() => {
        flushing = flushOutbox();
      });
      await waitFor(() => expect(__http.countFor(EVENTS)).toBe(1));

      auth.switchTo(sessionFor(USER_B, 'token-b'));

      await act(async () => {
        release();
        await flushing;
        await flushOutbox();
      });

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
      startOutboxOverDevice();

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
  let unregisterSearchStateReset: (() => void) | undefined;

  beforeAll(() => {
    unregisterSearchStateReset = registerSearchStateSignOutReset();
  });

  afterAll(() => {
    unregisterSearchStateReset?.();
  });

  const TAPPED: DiscoveryResult = {
    kind: 'track',
    title: 'Anti-Hero',
    subtitle: 'Taylor Swift',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: {},
  };

  let auth: ScriptedAuth;
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
    setSearchState('', '');
    runSignOutCleanups();
  });

  describe('search text and last-tapped result do not survive an identity change', () => {
    it.each([
      ['user A signs out', 'SIGNED_OUT', null],
      ['the device switches straight to user B', 'SIGNED_IN', sessionFor(USER_B)],
    ] as const)(
      '%s -> search-state and detail-handoff are cleared',
      async (_label, event, next) => {
        auth = bootSignedIn(sessionFor(USER_A));
        const { result } = renderHook(() => useSession(), {
          wrapper: makeWrapper(new QueryClient()),
        });
        await waitFor(() => expect(result.current.status).toBe('signed-in'));

        userASearchedAndTapped();
        auth.emit(event, next);

        expectNothingOfUserALeft();
      },
    );

    it('a token refresh for the same user keeps the search the user is in the middle of', async () => {
      auth = bootSignedIn(sessionFor(USER_A));
      const { result } = renderHook(() => useSession(), {
        wrapper: makeWrapper(new QueryClient()),
      });
      await waitFor(() => expect(result.current.status).toBe('signed-in'));

      userASearchedAndTapped();
      auth.emit('TOKEN_REFRESHED', sessionFor(USER_A));

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
        bootSignedIn(null, outcome);
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
