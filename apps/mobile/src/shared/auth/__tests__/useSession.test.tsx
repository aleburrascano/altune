import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, act } from '@testing-library/react-native';
import type { AuthChangeEvent, Session } from '@supabase/supabase-js';
import * as FileSystem from 'expo-file-system';

import { usePinnedStore } from '@shared/offline/pinnedStore';

import { useSession } from '../useSession';
import { supabase } from '../supabaseClient';
import { clearSessionExpired, getSessionExpired, markSessionExpired } from '../sessionExpired';

jest.mock('../supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn(), onAuthStateChange: jest.fn() } },
}));

const { __fs } = FileSystem as unknown as {
  __fs: { seedFile(uri: string, contents: string): void; allFiles(): Record<string, string> };
};

type Listener = (event: AuthChangeEvent, session: Session | null) => void;
type Subscription = { listener: Listener; unsubscribe: jest.Mock; active: boolean };

function installAuth() {
  const getSession = supabase.auth.getSession as jest.Mock;
  const onAuthStateChange = supabase.auth.onAuthStateChange as jest.Mock;
  const subscriptions: Subscription[] = [];

  onAuthStateChange.mockImplementation((cb: Listener) => {
    const subscription: Subscription = { listener: cb, unsubscribe: jest.fn(), active: true };
    subscription.unsubscribe.mockImplementation(() => {
      subscription.active = false;
    });
    subscriptions.push(subscription);
    return { data: { subscription: { unsubscribe: subscription.unsubscribe } } };
  });
  getSession.mockReturnValue(new Promise<never>(() => {}));

  return {
    getSession,
    subscriptions,
    emit(event: AuthChangeEvent, session: Session | null): void {
      const subscription = subscriptions[subscriptions.length - 1];
      if (!subscription) throw new Error('onAuthStateChange was never subscribed to');
      subscription.listener(event, session);
    },
  };
}

function makeSession(userId: string, overrides: Partial<Session> = {}): Session {
  return {
    access_token: `token-${userId}`,
    refresh_token: `refresh-${userId}`,
    expires_in: 3600,
    token_type: 'bearer',
    user: { id: userId } as unknown as Session['user'],
    ...overrides,
  } as Session;
}

function makeWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

const pendingUnmounts: (() => void)[] = [];

function renderSession(queryClient: QueryClient) {
  const rendered = renderHook(() => useSession(), { wrapper: makeWrapper(queryClient) });
  pendingUnmounts.push(rendered.unmount);
  return rendered;
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

const LIBRARY_KEY = ['library', 'tracks'];
const PLAYLIST_KEY = ['playlists', 'detail', 'p1'];
const LOOKUP_KEY = ['tracks', 'lookup', 't1'];

function pinnedUri(trackId: string): string {
  return `file:///document/offline-audio/${trackId}.mp3`;
}

function seedLocalData(queryClient: QueryClient, trackId = 't1'): void {
  queryClient.setQueryData(LIBRARY_KEY, ['seed']);
  queryClient.setQueryData(PLAYLIST_KEY, { id: 'p1' });
  queryClient.setQueryData(LOOKUP_KEY, { id: 'tracks-lookup' });
  markSessionExpired();
  __fs.seedFile(pinnedUri(trackId), 'audio-bytes');
  usePinnedStore.setState({
    entries: { [trackId]: { trackId, status: 'ready', uri: pinnedUri(trackId) } },
    queue: [],
    isWorking: false,
  });
}

function assertLocalDataWiped(queryClient: QueryClient, trackId = 't1'): void {
  expect(queryClient.getQueryData(LIBRARY_KEY)).toBeUndefined();
  expect(queryClient.getQueryData(PLAYLIST_KEY)).toBeUndefined();
  expect(queryClient.getQueryData(LOOKUP_KEY)).toBeUndefined();
  expect(queryClient.getQueryCache().getAll()).toHaveLength(0);
  expect(getSessionExpired()).toBe(false);
  expect(usePinnedStore.getState().entries).toEqual({});
  expect(__fs.allFiles()[pinnedUri(trackId)]).toBeUndefined();
}

function assertLocalDataIntact(queryClient: QueryClient, trackId = 't1'): void {
  expect(queryClient.getQueryData(LIBRARY_KEY)).toEqual(['seed']);
  expect(queryClient.getQueryData(PLAYLIST_KEY)).toEqual({ id: 'p1' });
  expect(queryClient.getQueryData(LOOKUP_KEY)).toEqual({ id: 'tracks-lookup' });
  expect(getSessionExpired()).toBe(true);
  expect(usePinnedStore.getState().entries).not.toEqual({});
  expect(__fs.allFiles()[pinnedUri(trackId)]).toBe('audio-bytes');
}

beforeEach(() => {
  jest.clearAllMocks();
  clearSessionExpired();
  usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
});

afterEach(() => {
  while (pendingUnmounts.length > 0) pendingUnmounts.pop()?.();
});

describe('Table: the identity-change branch across (seeded, previous, next)', () => {
  it('unseeded -> null (first observation is signed-out): no wipe', () => {
    const auth = installAuth();
    const queryClient = new QueryClient();
    const { result } = renderSession(queryClient);
    seedLocalData(queryClient);

    act(() => auth.emit('SIGNED_OUT', null));

    assertLocalDataIntact(queryClient);
    expect(result.current.status).toBe('signed-out');
  });

  it('unseeded -> A (first sign-in ever observed on this device): no wipe', () => {
    const auth = installAuth();
    const queryClient = new QueryClient();
    const { result } = renderSession(queryClient);
    seedLocalData(queryClient);

    act(() => auth.emit('SIGNED_IN', makeSession('user-a')));

    assertLocalDataIntact(queryClient);
    expect(result.current.status).toBe('signed-in');
  });

  it('A -> A (same user redelivered, e.g. a token refresh): no wipe', () => {
    const auth = installAuth();
    const queryClient = new QueryClient();
    renderSession(queryClient);
    act(() => auth.emit('SIGNED_IN', makeSession('user-a')));
    seedLocalData(queryClient);

    act(() => auth.emit('TOKEN_REFRESHED', makeSession('user-a')));

    assertLocalDataIntact(queryClient);
  });

  it('A -> B (a second account signs in with no intervening sign-out): wipes', () => {
    const auth = installAuth();
    const queryClient = new QueryClient();
    renderSession(queryClient);
    act(() => auth.emit('SIGNED_IN', makeSession('user-a')));
    seedLocalData(queryClient);

    act(() => auth.emit('SIGNED_IN', makeSession('user-b')));

    assertLocalDataWiped(queryClient);
  });

  it('A -> null (refresh token revoked or rotated server-side): wipes', () => {
    const auth = installAuth();
    const queryClient = new QueryClient();
    renderSession(queryClient);
    act(() => auth.emit('SIGNED_IN', makeSession('user-a')));
    seedLocalData(queryClient);

    act(() => auth.emit('SIGNED_OUT', null));

    assertLocalDataWiped(queryClient);
  });

  it('null -> A (signing back in after an observed sign-out): wipes', () => {
    const auth = installAuth();
    const queryClient = new QueryClient();
    renderSession(queryClient);
    act(() => auth.emit('SIGNED_OUT', null));
    seedLocalData(queryClient);

    act(() => auth.emit('SIGNED_IN', makeSession('user-a')));

    assertLocalDataWiped(queryClient);
  });
});

describe('Reducer: every AuthChangeEvent crossed with same-user vs different-user sessions', () => {
  const ALL_EVENTS: AuthChangeEvent[] = [
    'INITIAL_SESSION',
    'SIGNED_IN',
    'SIGNED_OUT',
    'TOKEN_REFRESHED',
    'USER_UPDATED',
    'PASSWORD_RECOVERY',
    'MFA_CHALLENGE_VERIFIED',
  ];

  it.each(ALL_EVENTS)(
    '%s redelivering the same user never wipes — the event name is not the discriminant, session identity is',
    (event) => {
      const auth = installAuth();
      const queryClient = new QueryClient();
      renderSession(queryClient);
      act(() => auth.emit('SIGNED_IN', makeSession('user-a')));
      seedLocalData(queryClient);

      act(() => auth.emit(event, makeSession('user-a')));

      assertLocalDataIntact(queryClient);
    },
  );

  it.each(ALL_EVENTS.filter((event) => event !== 'SIGNED_OUT'))(
    '%s carrying a different user always wipes',
    (event) => {
      const auth = installAuth();
      const queryClient = new QueryClient();
      renderSession(queryClient);
      act(() => auth.emit('SIGNED_IN', makeSession('user-a')));
      seedLocalData(queryClient);

      act(() => auth.emit(event, makeSession('user-b')));

      assertLocalDataWiped(queryClient);
    },
  );

  it('illegal pair: TOKEN_REFRESHED for the same user must never wipe', () => {
    const auth = installAuth();
    const queryClient = new QueryClient();
    renderSession(queryClient);
    act(() => auth.emit('SIGNED_IN', makeSession('user-a')));
    seedLocalData(queryClient);

    act(() => auth.emit('TOKEN_REFRESHED', makeSession('user-a')));

    assertLocalDataIntact(queryClient);
  });
});

describe('Live item 1: the identity-change branch guards against a cross-account data leak', () => {
  it('a revoked/rotated refresh token (SDK-emitted SIGNED_OUT) wipes the outgoing user’s cache and downloads', () => {
    const auth = installAuth();
    const queryClient = new QueryClient();
    renderSession(queryClient);
    act(() => auth.emit('SIGNED_IN', makeSession('user-a')));
    seedLocalData(queryClient);

    act(() => auth.emit('SIGNED_OUT', null));

    assertLocalDataWiped(queryClient);
  });

  it('a deep-link auth exchange into a second account on a shared device wipes the first account’s cache without an explicit sign-out', () => {
    const auth = installAuth();
    const queryClient = new QueryClient();
    const { result } = renderSession(queryClient);
    act(() => auth.emit('SIGNED_IN', makeSession('user-a')));
    seedLocalData(queryClient);

    act(() => auth.emit('SIGNED_IN', makeSession('user-b')));

    assertLocalDataWiped(queryClient);
    expect(result.current).toMatchObject({
      status: 'signed-in',
      session: { user: { id: 'user-b' } },
    });
  });
});

describe('Live item 2: seededRef distinguishes first observation from a real identity change', () => {
  it('an hourly token refresh for the same signed-in user never touches cache or downloads', () => {
    const auth = installAuth();
    const queryClient = new QueryClient();
    renderSession(queryClient);
    act(() => auth.emit('SIGNED_IN', makeSession('user-a')));
    seedLocalData(queryClient);

    act(() =>
      auth.emit('TOKEN_REFRESHED', makeSession('user-a', { access_token: 'rotated-token' })),
    );

    assertLocalDataIntact(queryClient);
  });

  it('the first real identity change after a cold start is detected, not missed, because seededRef flips true on the first observation', () => {
    const auth = installAuth();
    const queryClient = new QueryClient();
    renderSession(queryClient);
    act(() => auth.emit('SIGNED_IN', makeSession('user-a')));
    seedLocalData(queryClient);

    act(() => auth.emit('SIGNED_IN', makeSession('user-b')));

    assertLocalDataWiped(queryClient);
  });
});

describe('Live item 3: the initial getSession() seed, its failure fallback, and effect cleanup', () => {
  it('getSession() resolving with a session on cold start moves the hook out of loading into signed-in', async () => {
    const auth = installAuth();
    auth.getSession.mockResolvedValue({ data: { session: makeSession('user-a') } });
    const queryClient = new QueryClient();
    const { result } = renderSession(queryClient);
    expect(result.current).toEqual({ status: 'loading' });

    await flush();

    expect(result.current.status).toBe('signed-in');
  });

  it('getSession() rejecting on cold start (keychain unavailable) falls back to signed-out instead of hanging on loading forever', async () => {
    const auth = installAuth();
    auth.getSession.mockRejectedValue(new Error('secure store unavailable'));
    const queryClient = new QueryClient();
    const { result } = renderSession(queryClient);

    await flush();

    expect(result.current).toEqual({ status: 'signed-out' });
  });

  it('unmounting unsubscribes from onAuthStateChange exactly once', () => {
    const auth = installAuth();
    const queryClient = new QueryClient();
    const { unmount } = renderSession(queryClient);
    const subscription = auth.subscriptions[0];
    if (!subscription) throw new Error('expected a subscription to have been captured');

    unmount();

    expect(subscription.unsubscribe).toHaveBeenCalledTimes(1);
  });
});

describe('Concurrency: getSession() racing unmount, and the seed racing the listener', () => {
  it('a getSession() resolution arriving after unmount is discarded, not applied to the orphaned closure', async () => {
    const auth = installAuth();
    let resolveGetSession!: (value: { data: { session: Session | null } }) => void;
    auth.getSession.mockReturnValue(
      new Promise((resolve) => {
        resolveGetSession = resolve;
      }),
    );
    const queryClient = new QueryClient();
    const { unmount } = renderSession(queryClient);
    act(() => auth.emit('SIGNED_IN', makeSession('user-a')));
    seedLocalData(queryClient);

    unmount();
    resolveGetSession({ data: { session: makeSession('user-b') } });
    await flush();

    assertLocalDataIntact(queryClient);
  });

  it('when the listener seeds synchronously before getSession() resolves, the late resolution for the same user is a no-op, and the next real identity change is still detected', async () => {
    const auth = installAuth();
    let resolveGetSession!: (value: { data: { session: Session | null } }) => void;
    auth.getSession.mockReturnValue(
      new Promise((resolve) => {
        resolveGetSession = resolve;
      }),
    );
    const queryClient = new QueryClient();
    renderSession(queryClient);
    act(() => auth.emit('INITIAL_SESSION', makeSession('user-a')));
    seedLocalData(queryClient);

    resolveGetSession({ data: { session: makeSession('user-a') } });
    await flush();
    assertLocalDataIntact(queryClient);

    act(() => auth.emit('SIGNED_IN', makeSession('user-b')));

    assertLocalDataWiped(queryClient);
  });

  it('when getSession() resolves before the listener fires, the subsequent same-user listener event is a no-op, and the next real identity change is still detected', async () => {
    const auth = installAuth();
    auth.getSession.mockResolvedValue({ data: { session: makeSession('user-a') } });
    const queryClient = new QueryClient();
    renderSession(queryClient);
    await flush();
    seedLocalData(queryClient);

    act(() => auth.emit('TOKEN_REFRESHED', makeSession('user-a')));
    assertLocalDataIntact(queryClient);

    act(() => auth.emit('SIGNED_OUT', null));

    assertLocalDataWiped(queryClient);
  });
});

describe('Idempotence / replay: apply(apply(e)) equals apply(e) for a replayable session', () => {
  it('INITIAL_SESSION then SIGNED_IN carrying the identical session (the real cold-start sequence) does not wipe', () => {
    const auth = installAuth();
    const queryClient = new QueryClient();
    renderSession(queryClient);
    const session = makeSession('user-a');
    act(() => auth.emit('INITIAL_SESSION', session));
    seedLocalData(queryClient);

    act(() => auth.emit('SIGNED_IN', session));

    assertLocalDataIntact(queryClient);
  });

  it('a redelivered identical TOKEN_REFRESHED session does not wipe a second time', () => {
    const auth = installAuth();
    const queryClient = new QueryClient();
    renderSession(queryClient);
    act(() => auth.emit('SIGNED_IN', makeSession('user-a')));
    const refreshed = makeSession('user-a', { access_token: 'rotated-1' });
    act(() => auth.emit('TOKEN_REFRESHED', refreshed));
    seedLocalData(queryClient);

    act(() => auth.emit('TOKEN_REFRESHED', refreshed));

    assertLocalDataIntact(queryClient);
  });
});

describe('Adversarial: thin and malformed session payloads at the keychain/server trust boundary', () => {
  it('a session with no user object degrades to signed-out instead of throwing', () => {
    const auth = installAuth();
    const queryClient = new QueryClient();
    const { result } = renderSession(queryClient);
    const malformed = {
      access_token: 't',
      refresh_token: 'r',
      expires_in: 3600,
      token_type: 'bearer',
    } as unknown as Session;

    expect(() => act(() => auth.emit('SIGNED_IN', malformed))).not.toThrow();
    expect(result.current).toEqual({ status: 'signed-out' });
  });

  it('never reports signed-in with a session whose user is absent, because every consumer reads through session.user', () => {
    const auth = installAuth();
    const queryClient = new QueryClient();
    const { result } = renderSession(queryClient);
    act(() => auth.emit('SIGNED_IN', makeSession('user-a')));
    const malformed = { access_token: 't', refresh_token: 'r', expires_at: 1 } as unknown as Session;

    act(() => auth.emit('TOKEN_REFRESHED', malformed));

    expect(result.current.status).toBe('signed-out');
  });

  it('a user with an absent id is treated as a null identity, and still triggers the safe-side wipe when it replaces a real user', () => {
    const auth = installAuth();
    const queryClient = new QueryClient();
    renderSession(queryClient);
    act(() => auth.emit('SIGNED_IN', makeSession('user-a')));
    seedLocalData(queryClient);
    const thin = makeSession('user-a', { user: {} as unknown as Session['user'] });

    act(() => auth.emit('SIGNED_IN', thin));

    assertLocalDataWiped(queryClient);
  });

  it('a user with an empty-string id is a distinct identity from signed-out, and still triggers the wipe when it replaces a real user', () => {
    const auth = installAuth();
    const queryClient = new QueryClient();
    renderSession(queryClient);
    act(() => auth.emit('SIGNED_IN', makeSession('user-a')));
    seedLocalData(queryClient);
    const thin = makeSession('user-a', { user: { id: '' } as unknown as Session['user'] });

    act(() => auth.emit('SIGNED_IN', thin));

    assertLocalDataWiped(queryClient);
  });

  it('an expired session is passed through unchanged — this hook does not second-guess Supabase on expiry', () => {
    const auth = installAuth();
    const queryClient = new QueryClient();
    const { result } = renderSession(queryClient);
    const expired = makeSession('user-a', { expires_at: Math.floor(Date.now() / 1000) - 3600 });

    act(() => auth.emit('SIGNED_IN', expired));

    expect(result.current).toEqual({ status: 'signed-in', session: expired });
  });

  it(
    'a transient getSession() rejection must not wipe a session the listener already confirmed for the same user',
    async () => {
      const auth = installAuth();
      let rejectGetSession!: (error: unknown) => void;
      auth.getSession.mockReturnValue(
        new Promise((_resolve, reject) => {
          rejectGetSession = reject;
        }),
      );
      const queryClient = new QueryClient();
      renderSession(queryClient);
      act(() => auth.emit('INITIAL_SESSION', makeSession('user-a')));
      seedLocalData(queryClient);

      rejectGetSession(new Error('transient keychain read failure'));
      await flush();

      assertLocalDataIntact(queryClient);
    },
  );
});

describe('Invalidation: an identity change clears the exact cache entries and empties the pinned index, on disk too', () => {
  it('a different-user transition clears every seeded query key by identity, and empties the pinned index and its on-disk file — not just one of them', () => {
    const auth = installAuth();
    const queryClient = new QueryClient();
    renderSession(queryClient);
    act(() => auth.emit('SIGNED_IN', makeSession('user-a')));
    seedLocalData(queryClient);
    expect(queryClient.getQueryData(LIBRARY_KEY)).toBeDefined();
    expect(queryClient.getQueryData(PLAYLIST_KEY)).toBeDefined();
    expect(queryClient.getQueryData(LOOKUP_KEY)).toBeDefined();

    act(() => auth.emit('SIGNED_IN', makeSession('user-b')));

    assertLocalDataWiped(queryClient);
  });

  it('a same-user token refresh leaves every one of those exact keys, and the pinned file, untouched', () => {
    const auth = installAuth();
    const queryClient = new QueryClient();
    renderSession(queryClient);
    act(() => auth.emit('SIGNED_IN', makeSession('user-a')));
    seedLocalData(queryClient);

    act(() => auth.emit('TOKEN_REFRESHED', makeSession('user-a')));

    assertLocalDataIntact(queryClient);
  });
});
