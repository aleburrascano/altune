import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render } from '@testing-library/react-native';

import { asTrackId } from '@shared/api-client/ids';

jest.mock('@shared/api-client/audio', () => ({ fetchAudioUrls: jest.fn().mockResolvedValue([]) }));

let mockOfflineDownloadsSupported = true;
jest.mock('@shared/offline/offlineSupport', () => ({
  get offlineDownloadsSupported() {
    return mockOfflineDownloadsSupported;
  },
}));

const MODULES_MOCKED_PER_TEST = [
  '@shared/offline/pinnedIndex',
  '@shared/offline/pinnedDownloadWorker',
  '@shared/offline/pinnedStore',
  '@shared/telemetry/outbox',
  '@shared/session/signOutCleanup',
  '@shared/auth/supabaseClient',
  '@shared/auth/useSignOut',
  '@features/settings/hooks/useAccountEmail',
  '@features/settings/hooks/useDownloadStats',
  '@shared/api-client/tracks',
  '@shared/api-client/discovery',
];

afterEach(() => {
  for (const id of MODULES_MOCKED_PER_TEST) jest.dontMock(id);
  jest.doMock('@shared/offline/offlineSupport', () => ({
    get offlineDownloadsSupported() {
      return mockOfflineDownloadsSupported;
    },
  }));
});

function withOfflineSupport<T>(offlineDownloadsSupported: boolean, load: () => T): T {
  let loaded: T | undefined;
  jest.isolateModules(() => {
    jest.doMock('@shared/offline/offlineSupport', () => ({ offlineDownloadsSupported }));
    loaded = load();
  });
  return loaded as T;
}

describe('mh09: the pinned-store machinery never starts on web', () => {
  it('does not load the pinned index or run the download queue when unsupported', () => {
    const { usePinnedStore, loadIndexSpy, runDownloadQueue } = withOfflineSupport(false, () => {
      const loadIndexSpy = jest.fn(() => ({}));
      jest.doMock('@shared/offline/pinnedIndex', () => ({
        ...jest.requireActual('@shared/offline/pinnedIndex'),
        loadIndex: loadIndexSpy,
      }));
      const runDownloadQueue = jest.fn().mockResolvedValue(undefined);
      jest.doMock('@shared/offline/pinnedDownloadWorker', () => ({ runDownloadQueue }));
      return {
        usePinnedStore: require('@shared/offline/pinnedStore').usePinnedStore,
        loadIndexSpy,
        runDownloadQueue,
      };
    });

    expect(loadIndexSpy).not.toHaveBeenCalled();
    expect(usePinnedStore.getState().entries).toEqual({});

    usePinnedStore.getState().pin(asTrackId('t1'));
    expect(runDownloadQueue).not.toHaveBeenCalled();
  });

  it('loads the pinned index and runs the download queue on native, unaffected', () => {
    const { usePinnedStore, loadIndexSpy, runDownloadQueue } = withOfflineSupport(true, () => {
      const loadIndexSpy = jest.fn(() => ({}));
      jest.doMock('@shared/offline/pinnedIndex', () => ({
        ...jest.requireActual('@shared/offline/pinnedIndex'),
        loadIndex: loadIndexSpy,
      }));
      const runDownloadQueue = jest.fn().mockResolvedValue(undefined);
      jest.doMock('@shared/offline/pinnedDownloadWorker', () => ({ runDownloadQueue }));
      return {
        usePinnedStore: require('@shared/offline/pinnedStore').usePinnedStore,
        loadIndexSpy,
        runDownloadQueue,
      };
    });

    expect(loadIndexSpy).toHaveBeenCalledTimes(1);

    usePinnedStore.getState().pin(asTrackId('t1'));
    expect(runDownloadQueue).toHaveBeenCalled();
  });
});

describe('mh09: an identity change never claims pinned downloads on web', () => {
  function claimForSignedInUser(offlineDownloadsSupported: boolean): jest.Mock {
    return withOfflineSupport(offlineDownloadsSupported, () => {
      const claimPinnedDownloads = jest.fn();
      jest.doMock('@shared/offline/pinnedStore', () => ({ claimPinnedDownloads }));
      jest.doMock('@shared/telemetry/outbox', () => ({ setOutboxOwner: jest.fn() }));
      const handlers: Array<(userId: string | null) => void> = [];
      jest.doMock('@shared/session/signOutCleanup', () => ({
        onIdentityChange: (fn: (userId: string | null) => void) => handlers.push(fn),
      }));
      const { registerIdentityListeners } = require('@shared/auth/registerIdentityListeners');
      registerIdentityListeners();
      handlers.forEach((fn) => fn('user-a'));
      return claimPinnedDownloads;
    });
  }

  it('does not claim pinned downloads for a signed-in user on web', () => {
    expect(claimForSignedInUser(false)).not.toHaveBeenCalled();
  });

  it('claims pinned downloads for a signed-in user on native, unaffected', () => {
    expect(claimForSignedInUser(true)).toHaveBeenCalledWith('user-a');
  });
});

describe('mh09: an acquisition completing never restarts a stale pin on web', () => {
  function statusAfterCompletion(offlineDownloadsSupported: boolean): string | undefined {
    const { usePinnedStore, applyServerEvent } = withOfflineSupport(offlineDownloadsSupported, () => ({
      usePinnedStore: require('@shared/offline/pinnedStore').usePinnedStore,
      applyServerEvent: require('@shared/events/applyServerEvent').applyServerEvent,
    }));
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
  }

  it('leaves the stale download ready, never re-queued, on web', () => {
    expect(statusAfterCompletion(false)).toBe('ready');
  });

  it('re-queues the stale download on native, unaffected', () => {
    expect(statusAfterCompletion(true)).toBe('downloading');
  });
});

describe('mh09: the reconcile bridge does nothing on web', () => {
  let reconcile: jest.Mock;
  let OfflineReconcileBridge: (props: Record<string, never>) => React.ReactElement | null;

  beforeAll(() => {
    reconcile = jest.fn();
    jest.doMock('@shared/offline/pinnedStore', () => ({
      usePinnedStore: (selector: (s: { reconcile: () => void }) => unknown) =>
        selector({ reconcile }),
    }));
    OfflineReconcileBridge = require('@shared/offline/OfflineReconcileBridge').OfflineReconcileBridge;
  });

  beforeEach(() => {
    reconcile.mockClear();
  });

  function renderBridge(offlineDownloadsSupported: boolean) {
    mockOfflineDownloadsSupported = offlineDownloadsSupported;
    render(React.createElement(OfflineReconcileBridge));
    return reconcile;
  }

  it('never reconciles on web', () => {
    expect(renderBridge(false)).not.toHaveBeenCalled();
  });

  it('reconciles once on mount on native, unaffected', () => {
    expect(renderBridge(true)).toHaveBeenCalledTimes(1);
  });
});

describe('mh09: the settings screen omits the offline downloads card on web', () => {
  let SettingsScreen: () => React.ReactElement;

  beforeAll(() => {
    jest.doMock('@shared/auth/supabaseClient', () => ({
      supabase: {
        auth: { getSession: jest.fn().mockResolvedValue({ data: { session: null }, error: null }) },
      },
    }));
    jest.doMock('@shared/auth/useSignOut', () => ({
      useSignOut: () => ({ state: { status: 'idle' }, signOut: jest.fn() }),
    }));
    jest.doMock('@features/settings/hooks/useAccountEmail', () => ({
      useAccountEmail: () => 'me@example.com',
    }));
    jest.doMock('@features/settings/hooks/useDownloadStats', () => ({
      ...jest.requireActual('@features/settings/hooks/useDownloadStats'),
      useDownloadStats: () => ({
        downloadCount: 0,
        downloadBytes: 0,
        downloadSize: '0 B',
        usageLabel: 'No downloads on this device',
        usageDetail: undefined,
      }),
    }));
    jest.doMock('@shared/api-client/tracks', () => ({
      ...jest.requireActual('@shared/api-client/tracks'),
      backfillFeaturedArtists: jest.fn().mockResolvedValue({ updated: 0, scanned: 0 }),
    }));
    jest.doMock('@shared/api-client/discovery', () => ({
      ...jest.requireActual('@shared/api-client/discovery'),
      clearSearchHistory: jest.fn().mockResolvedValue(undefined),
    }));
    SettingsScreen = require('@features/settings/ui/SettingsScreen').SettingsScreen;
  });

  function renderSettingsScreen(offlineDownloadsSupported: boolean) {
    mockOfflineDownloadsSupported = offlineDownloadsSupported;
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    return render(
      React.createElement(QueryClientProvider, { client }, React.createElement(SettingsScreen)),
    );
  }

  it('renders no offline downloads card on web', () => {
    renderSettingsScreen(false);
    expect(
      require('@testing-library/react-native').screen.queryByTestId('settings-downloads-usage'),
    ).toBeNull();
  });

  it('renders the offline downloads card on native, unaffected', () => {
    renderSettingsScreen(true);
    expect(
      require('@testing-library/react-native').screen.getByTestId('settings-downloads-usage'),
    ).toBeTruthy();
  });
});
