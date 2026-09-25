import React, { type ReactNode } from 'react';
import * as ReactQuery from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';
import type { Session } from '@supabase/supabase-js';

import type * as PinnedStoreModule from '@shared/offline/pinnedStore';

import type * as SupabaseClientModule from '../supabaseClient';
import type * as UseSessionModule from '../useSession';
import { asTrackId } from '@shared/api-client/ids';

// #835: pinned downloads are keyed by trackId only. When the app dies before the
// sign-out cleanup finishes, pinned.json and the audio survive, and the next
// account to sign in on the device must not be able to see or play them.

jest.mock('@shared/api-client/audio', () => ({ fetchAudioUrls: jest.fn() }));

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
