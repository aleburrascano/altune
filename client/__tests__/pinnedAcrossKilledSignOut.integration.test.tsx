import type { ReactNode } from 'react';
import type * as React from 'react';
import type { Session } from '@supabase/supabase-js';
import type * as ReactQuery from '@tanstack/react-query';
import type * as RenderTesting from '@testing-library/react-native/pure';

import type * as AuthDoor from '@shared/auth';
import { asTrackId } from '@shared/catalog';
import type * as OfflineDoor from '@shared/offline';
import { scriptedAuth } from '@testing/scriptedAuth';

const { __http } = require('@testing/doubles/fetch.js');

describe('pinned downloads across a killed sign-out', () => {
  type FsDouble = {
    seedFile(uri: string, contents: string): void;
    readFile(uri: string): string | undefined;
    allFiles(): Record<string, string>;
  };

  type App = {
    react: typeof React;
    query: typeof ReactQuery;
    testing: typeof RenderTesting;
    auth: typeof AuthDoor;
    pinned: typeof OfflineDoor;
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

  function bootApp(): App {
    const app: App = {
      react: require('react'),
      query: require('@tanstack/react-query'),
      testing: require('@testing-library/react-native/pure'),
      auth: require('@shared/auth'),
      pinned: require('@shared/offline'),
    };
    app.pinned.provideOfflinePort(require('@testing/apiClientPorts').apiClientOfflinePort);
    return app;
  }

  function killAndRelaunch(): App {
    const snapshot = currentFs().allFiles();
    jest.resetModules();
    const fs = currentFs();
    for (const [uri, contents] of Object.entries(snapshot)) fs.seedFile(uri, contents);
    return bootApp();
  }

  async function signIn(app: App, userId: string): Promise<() => void> {
    scriptedAuth(app.auth.installAuthClient, sessionFor(userId));
    const client = new app.query.QueryClient();
    const wrapper = ({ children }: { children: ReactNode }) =>
      app.react.createElement(app.query.QueryClientProvider, { client }, children);
    const hook = app.testing.renderHook(() => app.auth.useSession(), { wrapper });
    await app.testing.waitFor(() => expect(hook.result.current.status).toBe('signed-in'));
    return hook.unmount;
  }

  async function pinAsReady(app: App, trackId: string): Promise<void> {
    __http.reply('POST /v1/audio-urls', {
      status: 200,
      json: {
        urls: [{ track_id: trackId, url: `https://cdn.example.com/${trackId}.mp3`, version: 'v1' }],
      },
    });
    app.pinned.usePinnedStore.getState().pin(asTrackId(trackId));
    await app.testing.waitFor(() =>
      expect(app.pinned.usePinnedStore.getState().entries[trackId]?.status).toBe('ready'),
    );
  }

  beforeEach(() => {
    jest.resetModules();
  });

  describe('pinned downloads never cross accounts after a killed sign-out', () => {
    it('B signing in after A was killed mid sign-out sees and plays none of A downloads', async () => {
      const first = bootApp();
      const unmountA = await signIn(first, USER_A);
      await pinAsReady(first, 't1');
      unmountA();
      expect(currentFs().readFile(AUDIO_URI)).toBeDefined();

      const second = killAndRelaunch();
      expect(second.pinned.usePinnedStore.getState().entries['t1']).toBeDefined();

      const unmountB = await signIn(second, USER_B);

      expect(second.pinned.usePinnedStore.getState().entries).toEqual({});
      expect(second.pinned.resolvePinnedUri(asTrackId('t1'))).toBeUndefined();
      expect(second.pinned.pinnedBytes()).toBe(0);
      expect(currentFs().allFiles()[AUDIO_URI]).toBeUndefined();
      expect(JSON.parse(currentFs().readFile(INDEX_URI) ?? 'null')).toEqual({
        schemaVersion: 1,
        entries: {},
      });
      unmountB();

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

      const unmountB = await signIn(app, USER_B);

      expect(app.pinned.resolvePinnedUri(asTrackId('t1'))).toBeUndefined();
      expect(currentFs().allFiles()[AUDIO_URI]).toBeUndefined();
      unmountB();
    });
  });
});
