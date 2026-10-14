import * as React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react-native';

import { asTrackId } from '@shared/apiClient/ids';
import { applyServerEvent } from '@shared/events/applyServerEvent';
import { notifyIdentityChange } from '@shared/session/signOutCleanup';
import { readOwner } from '@shared/offline/pinnedIndex';
import { usePinnedStore } from '@shared/offline/pinnedStore';
import { OfflineReconcileBridge } from '@shared/offline/ui/OfflineReconcileBridge';
import { FeaturePorts } from '@/wiring/FeaturePorts';
import { httpFeaturePorts } from '@/wiring/httpFeaturePorts';
import { SettingsScreen } from '@features/settings/ui/SettingsScreen';

const INDEX_URI = 'file:///document/offline/pinned.json';

function newQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
}

describe('mh09: the pinned-store machinery never starts on web', () => {
  it('does not load the pinned index or run the download queue when unsupported', () => {
    let loaded: unknown;
    let working: boolean | undefined;
    jest.isolateModules(() => {
      require('expo-file-system').__fs.seedFile(
        INDEX_URI,
        JSON.stringify({ old: { trackId: 'old', status: 'failed' } }),
      );
      const store = require('@shared/offline/pinnedStore').usePinnedStore;
      loaded = { ...store.getState().entries };
      store.getState().pin(asTrackId('t1'));
      working = store.getState().isWorking;
    });

    expect(loaded).toEqual({});
    expect(working).toBe(false);
  });
});

describe('mh09: an identity change never claims pinned downloads on web', () => {
  it('does not claim pinned downloads for a signed-in user on web', () => {
    notifyIdentityChange('user-a');

    expect(readOwner()).toBeNull();
  });
});

describe('mh09: an acquisition completing never restarts a stale pin on web', () => {
  it('leaves the stale download ready, never re-queued, on web', () => {
    usePinnedStore.setState({
      entries: { t1: { trackId: asTrackId('t1'), status: 'ready', uri: 'file:///stale.mp3' } },
      queue: [],
      isWorking: false,
    });

    applyServerEvent(newQueryClient(), {
      id: '1',
      type: 'track_acquisition_completed',
      data: { track_id: 't1', audio_ref: 'ref-1' },
    });

    expect(usePinnedStore.getState().entries.t1?.status).toBe('ready');
  });
});

describe('mh09: the reconcile bridge does nothing on web', () => {
  it('leaves a ready entry whose file is missing unchanged on web', () => {
    usePinnedStore.setState({
      entries: { t1: { trackId: asTrackId('t1'), status: 'ready', uri: 'file:///missing.mp3' } },
      queue: [],
      isWorking: true,
    });

    render(React.createElement(OfflineReconcileBridge));

    expect(usePinnedStore.getState().entries.t1?.status).toBe('ready');
  });
});

describe('mh09: the settings screen omits the offline downloads card on web', () => {
  it('renders no offline downloads card on web', () => {
    render(
      React.createElement(
        QueryClientProvider,
        { client: newQueryClient() },
        React.createElement(
          FeaturePorts,
          { ports: httpFeaturePorts },
          React.createElement(SettingsScreen),
        ),
      ),
    );

    expect(screen.queryByTestId('settings-downloads-usage')).toBeNull();
  });
});
