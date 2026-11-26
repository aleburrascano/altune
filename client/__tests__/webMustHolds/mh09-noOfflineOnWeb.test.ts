import * as React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react-native';
import { asTrackId } from '@shared/catalog';
import { OfflineReconcileBridge, usePinnedStore } from '@shared/offline';
import { transientRetryOptions } from '@shared/query';
import { FeaturePorts } from '@/mobile/wiring/FeaturePorts';
import { httpFeaturePorts } from '@/mobile/wiring/httpFeaturePorts';
import { SettingsScreen } from '@features/settings';

const { ensureAppAuthClient } = require('@testing/appAuthClient.js') as {
  ensureAppAuthClient: () => void;
};

const INDEX_URI = 'file:///document/offline/pinned.json';

function loadFresh<T>(load: () => T): T {
  let loaded!: T;
  jest.isolateModules(() => {
    ensureAppAuthClient();
    loaded = load();
  });
  return loaded;
}

describe('mh09: the pinned-store machinery never starts on web', () => {
  function pinOn() {
    return loadFresh(() => {
      require('expo-file-system').__fs.seedFile(
        INDEX_URI,
        JSON.stringify({ old: { trackId: 'old', status: 'failed' } }),
      );
      const { usePinnedStore } = require('@shared/offline');
      require('@shared/offline').provideOfflinePort(
        require('@testing/apiClientPorts').apiClientOfflinePort,
      );
      const loaded = { ...usePinnedStore.getState().entries };
      usePinnedStore.getState().pin(asTrackId('t1'));
      return { loaded, afterPin: usePinnedStore.getState().entries.t1?.status };
    });
  }

  it('loads the pinned index and runs the download queue on native, unaffected', () => {
    const { loaded, afterPin } = pinOn();

    expect(Object.keys(loaded)).toEqual(['old']);
    expect(afterPin).toBe('downloading');
  });
});

describe('mh09: an identity change never claims pinned downloads on web', () => {
  function ownerAfterSignIn(): string | null {
    return loadFresh(() => {
      require('@shared/offline');
      require('@shared/session/signOutCleanup').notifyIdentityChange('user-a');
      return require('@shared/offline').readOwner();
    });
  }

  it('claims pinned downloads for a signed-in user on native, unaffected', () => {
    expect(ownerAfterSignIn()).toBe('user-a');
  });
});

describe('mh09: an acquisition completing never restarts a stale pin on web', () => {
  function statusAfterCompletion(): string | undefined {
    return loadFresh(() => {
      const { usePinnedStore } = require('@shared/offline');
      const { applyServerEvent } = require('@shared/events/shell/applyServerEvent');
      require('@shared/offline').provideOfflinePort(
        require('@testing/apiClientPorts').apiClientOfflinePort,
      );
      const { QueryClient } = require('@tanstack/react-query');
      usePinnedStore.setState({
        entries: { t1: { trackId: asTrackId('t1'), status: 'ready', uri: 'file:///stale.mp3' } },
        queue: [],
        isWorking: false,
      });
      const queryClient = new QueryClient({
        defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
      });
      applyServerEvent(queryClient, {
        id: '1',
        type: 'track_acquisition_completed',
        data: { track_id: 't1', audio_ref: 'ref-1' },
      });
      return usePinnedStore.getState().entries.t1?.status;
    });
  }

  it('re-queues the stale download on native, unaffected', () => {
    expect(statusAfterCompletion()).toBe('downloading');
  });
});

describe('mh09: the reconcile bridge does nothing on web', () => {
  function ConnectedBridge() {
    const reconcile = usePinnedStore((state) => state.reconcile);
    return React.createElement(OfflineReconcileBridge, { reconcile });
  }

  function statusAfterMount(): string | undefined {
    usePinnedStore.setState({
      entries: { t1: { trackId: asTrackId('t1'), status: 'ready', uri: 'file:///missing.mp3' } },
      queue: [],
      isWorking: true,
    });
    render(React.createElement(ConnectedBridge));
    return usePinnedStore.getState().entries.t1?.status;
  }

  it('drops a ready entry whose file is missing on native, unaffected', () => {
    expect(statusAfterMount()).toBeUndefined();
  });
});

describe('mh09: the settings screen omits the offline downloads card on web', () => {
  function hasDownloadsCard(): boolean {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    render(
      React.createElement(
        QueryClientProvider,
        { client },
        React.createElement(
          FeaturePorts,
          { ports: httpFeaturePorts },
          React.createElement(SettingsScreen, { retryOptions: transientRetryOptions }),
        ),
      ),
    );
    return screen.queryByTestId('settings-downloads-usage') !== null;
  }

  it('renders the offline downloads card on native, unaffected', () => {
    expect(hasDownloadsCard()).toBe(true);
  });
});
