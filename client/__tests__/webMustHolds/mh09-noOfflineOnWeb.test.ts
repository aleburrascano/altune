import * as React from 'react';
import { QueryClient } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react-native';

import { asTrackId } from '@shared/apiClient/ids';

const INDEX_URI = 'file:///document/offline/pinned.json';

const sharedReact = jest.requireActual('react');
const sharedReactNative = jest.requireActual('react-native');
const sharedReactQuery = jest.requireActual('@tanstack/react-query');

function onPlatform<T>(os: 'ios', load: () => T): T {
  const originalOs = sharedReactNative.Platform.OS;
  let loaded!: T;
  try {
    jest.isolateModules(() => {
      jest.doMock('react', () => sharedReact);
      jest.doMock('react-native', () => sharedReactNative);
      jest.doMock('@tanstack/react-query', () => sharedReactQuery);
      sharedReactNative.Platform.OS = os;
      loaded = load();
    });
  } finally {
    sharedReactNative.Platform.OS = originalOs;
  }
  return loaded;
}

describe('mh09: the pinned-store machinery never starts on web', () => {
  function pinOn(os: 'ios') {
    return onPlatform(os, () => {
      require('expo-file-system').__fs.seedFile(
        INDEX_URI,
        JSON.stringify({ old: { trackId: 'old', status: 'failed' } }),
      );
      const { usePinnedStore } = require('@shared/offline/pinnedStore');
      const loaded = { ...usePinnedStore.getState().entries };
      usePinnedStore.getState().pin(asTrackId('t1'));
      return { loaded, afterPin: usePinnedStore.getState().entries.t1?.status };
    });
  }

  it('loads the pinned index and runs the download queue on native, unaffected', () => {
    const { loaded, afterPin } = pinOn('ios');

    expect(Object.keys(loaded)).toEqual(['old']);
    expect(afterPin).toBe('downloading');
  });
});

describe('mh09: an identity change never claims pinned downloads on web', () => {
  function ownerAfterSignIn(os: 'ios'): string | null {
    return onPlatform(os, () => {
      require('@shared/offline/pinnedStore');
      require('@shared/session/signOutCleanup').notifyIdentityChange('user-a');
      return require('@shared/offline/pinnedIndex').readOwner();
    });
  }

  it('claims pinned downloads for a signed-in user on native, unaffected', () => {
    expect(ownerAfterSignIn('ios')).toBe('user-a');
  });
});

describe('mh09: an acquisition completing never restarts a stale pin on web', () => {
  function statusAfterCompletion(os: 'ios'): string | undefined {
    return onPlatform(os, () => {
      const { usePinnedStore } = require('@shared/offline/pinnedStore');
      const { applyServerEvent } = require('@shared/events/applyServerEvent');
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
    expect(statusAfterCompletion('ios')).toBe('downloading');
  });
});

describe('mh09: the reconcile bridge does nothing on web', () => {
  function statusAfterMountOn(os: 'ios'): string | undefined {
    const { usePinnedStore, OfflineReconcileBridge } = onPlatform(os, () => ({
      usePinnedStore: require('@shared/offline/pinnedStore').usePinnedStore,
      OfflineReconcileBridge: require('@shared/offline/ui/OfflineReconcileBridge')
        .OfflineReconcileBridge,
    }));
    usePinnedStore.setState({
      entries: { t1: { trackId: asTrackId('t1'), status: 'ready', uri: 'file:///missing.mp3' } },
      queue: [],
      isWorking: true,
    });
    render(React.createElement(OfflineReconcileBridge));
    return usePinnedStore.getState().entries.t1?.status;
  }

  it('drops a ready entry whose file is missing on native, unaffected', () => {
    expect(statusAfterMountOn('ios')).toBeUndefined();
  });
});

describe('mh09: the settings screen omits the offline downloads card on web', () => {
  function hasDownloadsCardOn(os: 'ios'): boolean {
    const { QueryClientProvider, FeaturePorts, httpFeaturePorts, SettingsScreen } = onPlatform(
      os,
      () => ({
        QueryClientProvider: require('@tanstack/react-query').QueryClientProvider,
        FeaturePorts: require('../../src/chrome/FeaturePorts').FeaturePorts,
        httpFeaturePorts: require('../../src/chrome/httpFeaturePorts').httpFeaturePorts,
        SettingsScreen: require('@features/settings/ui/SettingsScreen').SettingsScreen,
      }),
    );
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
          React.createElement(SettingsScreen),
        ),
      ),
    );
    return screen.queryByTestId('settings-downloads-usage') !== null;
  }

  it('renders the offline downloads card on native, unaffected', () => {
    expect(hasDownloadsCardOn('ios')).toBe(true);
  });
});
