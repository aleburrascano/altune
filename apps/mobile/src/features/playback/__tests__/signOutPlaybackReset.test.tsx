import React from 'react';
import type { Session } from '@supabase/supabase-js';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import * as FileSystem from 'expo-file-system';
import TrackPlayer, { Event } from 'react-native-track-player';

import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';
import { asTrackId } from '@shared/api-client/ids';
import { useSession } from '@shared/auth/useSession';
import { supabase } from '@shared/auth/supabaseClient';
import { useQueueStore } from '@shared/playback/queueStore';
import { usePlayback } from '@shared/playback/usePlayback';

import { prefetchNext } from '../audioPrefetch';
import { TrackPlayerPlaybackProvider } from '../hooks/trackPlayerProvider';
import { registerPlaybackService } from '../registerPlaybackService';
import { playbackService } from '../service';

import { libraryTrack } from './fixtures';

jest.mock('@shared/api-client/audio', () => ({
  ...jest.requireActual('@shared/api-client/audio'),
  fetchAudioUrls: jest.fn(),
}));

const { __player } = jest.requireMock('react-native-track-player');

type AuthCallback = (event: string, session: Session | null) => void;

const USER_A = { id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa' } as Session['user'];
const USER_B = { id: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb' } as Session['user'];

const A_TRACK = libraryTrack({ title: 'Private Track Of A' });

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

let authCallbacks: AuthCallback[] = [];

async function bootSession(initial: Session | null) {
  jest
    .spyOn(supabase.auth, 'getSession')
    .mockResolvedValue({ data: { session: initial }, error: null } as never);
  jest.spyOn(supabase.auth, 'onAuthStateChange').mockImplementation(((cb: AuthCallback) => {
    authCallbacks.push(cb);
    return { data: { subscription: { unsubscribe: jest.fn() } } };
  }) as never);
  const queryClient = new QueryClient();
  const hook = renderHook(() => useSession(), {
    wrapper: ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  });
  await waitFor(() => expect(hook.result.current.status).not.toBe('loading'));
  return hook;
}

function emitAuth(event: string, session: Session | null): void {
  act(() => {
    authCallbacks.forEach((cb) => cb(event, session));
  });
}

function remoteHandler(event: string): (data?: unknown) => void {
  const registration = __player
    .calls('addEventListener')
    .find(([registered]: [unknown]) => registered === event);
  if (!registration) throw new Error(`no ${event} listener was registered`);
  return registration[1];
}

async function flushNativeQueue(): Promise<void> {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

beforeEach(() => {
  authCallbacks = [];
  jest.restoreAllMocks();
  act(() => {
    useQueueStore.getState().clearQueue();
  });
});

describe('sign-out resets the previous user playback (#827)', () => {
  it("after A signs out mid-playback and B signs in, neither the queue store nor the native player keep A's track", async () => {
    registerPlaybackService();
    const session = await bootSession(sessionFor(USER_A));

    act(() => {
      useQueueStore.getState().loadQueue([A_TRACK], 0, null);
    });
    expect(useQueueStore.getState().currentTrack()).toEqual(A_TRACK);

    emitAuth('SIGNED_OUT', null);
    emitAuth('SIGNED_IN', sessionFor(USER_B));
    await flushNativeQueue();

    const queue = useQueueStore.getState();
    expect(queue.tracks).toEqual([]);
    expect(queue.currentIndex).toBe(-1);
    expect(queue.currentTrack()).toBeNull();
    // The native queue (A's signed URL + auth header) is dropped.
    expect(__player.calls('reset').length).toBeGreaterThanOrEqual(1);

    session.unmount();
  });

  it("a direct account switch (A to B with no signed-out step) also drops A's queue", async () => {
    registerPlaybackService();
    const session = await bootSession(sessionFor(USER_A));
    act(() => {
      useQueueStore.getState().loadQueue([A_TRACK], 0, null);
    });

    emitAuth('SIGNED_IN', sessionFor(USER_B));
    await flushNativeQueue();

    expect(useQueueStore.getState().tracks).toEqual([]);
    expect(__player.calls('reset')).toHaveLength(1);

    session.unmount();
  });

  it("a mounted player stops displaying A's track once B signs in directly", async () => {
    registerPlaybackService();
    const session = await bootSession(sessionFor(USER_A));
    const player = renderHook(() => usePlayback().track, {
      wrapper: ({ children }: { children: React.ReactNode }) => (
        <QueryClientProvider client={new QueryClient()}>
          <TrackPlayerPlaybackProvider>{children}</TrackPlayerPlaybackProvider>
        </QueryClientProvider>
      ),
    });
    act(() => {
      useQueueStore.getState().loadQueue([A_TRACK], 0, null);
    });
    expect(player.result.current?.title).toBe('Private Track Of A');

    emitAuth('SIGNED_IN', sessionFor(USER_B));
    await flushNativeQueue();

    expect(player.result.current).toBeNull();

    player.unmount();
    session.unmount();
  });

  it('a token refresh for the same user keeps the queue and the native player untouched', async () => {
    registerPlaybackService();
    const session = await bootSession(sessionFor(USER_A));
    act(() => {
      useQueueStore.getState().loadQueue([A_TRACK], 0, null);
    });

    emitAuth('TOKEN_REFRESHED', sessionFor(USER_A));
    await flushNativeQueue();

    expect(useQueueStore.getState().currentTrack()).toEqual(A_TRACK);
    expect(__player.calls('reset')).toHaveLength(0);

    session.unmount();
  });
});

// The queue store is cleared before the native reset is even attempted, so a rejected
// reset is invisible from the JS side: only the calls the native player was handed, and
// what was logged about them, say whether the outgoing user's queue is really gone.
describe('a native reset that fails during sign-out (#1728)', () => {
  let warn: jest.SpyInstance;

  beforeEach(() => {
    warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    warn.mockRestore();
  });

  function nativeError(code: string, message: string): Error {
    return Object.assign(new Error(message), { code });
  }

  async function signOutOfA(): Promise<{ unmount: () => void }> {
    registerPlaybackService();
    const session = await bootSession(sessionFor(USER_A));
    act(() => {
      useQueueStore.getState().loadQueue([A_TRACK], 0, null);
    });
    emitAuth('SIGNED_OUT', null);
    await flushNativeQueue();
    return session;
  }

  it("retries the reset, so one stalled bridge call cannot leave A's queue on the player", async () => {
    __player.failNext('reset', new Error('bridge stalled'));

    const session = await signOutOfA();

    expect(__player.calls('reset')).toHaveLength(2);
    expect(warn).toHaveBeenCalledWith(
      '[playback] native queue mutation failed',
      expect.objectContaining({ op: 'signOutReset', kind: 'transient' }),
    );

    session.unmount();
  });

  it('reports a reset that never lands, classified, rather than discarding the rejection', async () => {
    const stuck = nativeError('player_not_initialized', 'not initialized');
    const nativeReset = TrackPlayer.reset as jest.Mock;
    nativeReset.mockRejectedValueOnce(stuck).mockRejectedValueOnce(stuck);

    const session = await signOutOfA();

    expect(warn).toHaveBeenCalledWith(
      '[playback] native queue mutation failed',
      expect.objectContaining({
        op: 'signOutResetRetry',
        kind: 'permanent',
        code: 'player_not_initialized',
      }),
    );
    // Bounded: a stuck native player is reported, not hammered.
    expect(__player.calls('reset')).toHaveLength(2);

    session.unmount();
  });
});

// The queue and the native player are cleared on sign-out, but the prefetched audio is a file
// on disk: nothing in the sign-out path used to touch it, and `evict` only runs off a *later*
// prefetch, so A's tracks stayed readable to a forensic dump or a second profile until B's
// playback happened to trigger an eviction pass (#1722).
describe("A's prefetched audio leaves the device with A (#1722)", () => {
  const CACHE_DIR_URI = 'file:///cache/audio-prefetch';
  const NEXT_ID = 'trk-of-a-2';

  const { __fs, File } = FileSystem as unknown as {
    __fs: { seedFile(uri: string, contents: string): void; allFiles(): Record<string, string> };
    File: {
      new (uri: string): { uri: string };
      downloadFileAsync: (url: string, dest: { uri: string }) => Promise<{ uri: string }>;
    };
  };
  const fetchUrls = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

  function cachedNames(): string[] {
    return Object.keys(__fs.allFiles())
      .filter((uri) => uri.startsWith(`${CACHE_DIR_URI}/`))
      .map((uri) => uri.slice(CACHE_DIR_URI.length + 1))
      .sort();
  }

  function deferred(): { promise: Promise<void>; resolve: () => void } {
    let resolve!: () => void;
    const promise = new Promise<void>((r) => {
      resolve = r;
    });
    return { promise, resolve };
  }

  beforeEach(() => {
    fetchUrls.mockReset();
  });

  it('wipes the audio-prefetch directory when A signs out', async () => {
    registerPlaybackService();
    const session = await bootSession(sessionFor(USER_A));
    __fs.seedFile(`${CACHE_DIR_URI}/trk-of-a.v1.mp3`, "A's audio");

    emitAuth('SIGNED_OUT', null);
    await flushNativeQueue();

    expect(cachedNames()).toEqual([]);

    session.unmount();
  });

  it('wipes it on a direct switch from A to B, with no signed-out step', async () => {
    registerPlaybackService();
    const session = await bootSession(sessionFor(USER_A));
    __fs.seedFile(`${CACHE_DIR_URI}/trk-of-a.v1.mp3`, "A's audio");

    emitAuth('SIGNED_IN', sessionFor(USER_B));
    await flushNativeQueue();

    expect(cachedNames()).toEqual([]);

    session.unmount();
  });

  it('leaves nothing behind when a download that outlives its abort finally lands', async () => {
    registerPlaybackService();
    const session = await bootSession(sessionFor(USER_A));
    act(() => {
      useQueueStore
        .getState()
        .loadQueue(
          [A_TRACK, libraryTrack({ source: { kind: 'library', trackId: asTrackId(NEXT_ID) } })],
          0,
          null,
        );
    });
    fetchUrls.mockResolvedValueOnce([
      { trackId: NEXT_ID, url: `https://cdn.example/${NEXT_ID}.mp3`, version: 'v1' },
    ] satisfies ResolvedAudioUrl[]);
    const started = deferred();
    const finishes = deferred();
    jest.spyOn(File, 'downloadFileAsync').mockImplementation(async (_url, dest) => {
      started.resolve();
      await finishes.promise;
      __fs.seedFile(dest.uri, "A's audio");
      return new File(dest.uri);
    });

    const prefetch = prefetchNext(0);
    await started.promise;
    emitAuth('SIGNED_OUT', null);
    await flushNativeQueue();
    finishes.resolve();
    await prefetch;
    await flushNativeQueue();

    expect(cachedNames()).toEqual([]);

    session.unmount();
  });
});

describe('remote controls are inert while no user is signed in (#827)', () => {
  it('RemotePlay, RemoteNext, RemotePrevious and RemoteSeek do nothing after sign-out', async () => {
    await playbackService();
    const session = await bootSession(sessionFor(USER_A));
    emitAuth('SIGNED_OUT', null);
    __player.setProgress({ position: 1 });

    remoteHandler(Event.RemotePlay)();
    remoteHandler(Event.RemoteNext)();
    remoteHandler(Event.RemotePrevious)();
    remoteHandler(Event.RemoteSeek)({ position: 30 });
    remoteHandler(Event.RemoteDuck)({ paused: false, permanent: false });
    await flushNativeQueue();

    expect(__player.calls('play')).toHaveLength(0);
    expect(__player.calls('skipToNext')).toHaveLength(0);
    expect(__player.calls('skipToPrevious')).toHaveLength(0);
    expect(__player.calls('seekTo')).toHaveLength(0);

    session.unmount();
  });

  it('RemotePause still pauses while signed out, so audio can always be stopped', async () => {
    await playbackService();
    const session = await bootSession(null);

    remoteHandler(Event.RemotePause)();

    expect(__player.calls('pause')).toHaveLength(1);

    session.unmount();
  });

  it('RemotePlay resumes again once the next user has signed in', async () => {
    await playbackService();
    const session = await bootSession(sessionFor(USER_A));
    emitAuth('SIGNED_OUT', null);

    remoteHandler(Event.RemotePlay)();
    expect(__player.calls('play')).toHaveLength(0);

    emitAuth('SIGNED_IN', sessionFor(USER_B));
    remoteHandler(Event.RemotePlay)();
    expect(__player.calls('play')).toHaveLength(1);

    session.unmount();
  });
});
