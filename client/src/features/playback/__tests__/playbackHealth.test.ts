import TrackPlayer, { Event } from 'react-native-track-player';

import { useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import { recordEvent } from '@shared/telemetry/recordEvent';

import { prefetchNext } from '@features/playback/native/audioPrefetch';
import { reportQueueFailure } from '@features/playback/native/queueFailureReport';
import { loadNativeQueue } from '@features/playback/native/loadNativeTrack';
import { forgetAllSwaps } from '@features/playback/native/nativeTrackSwap';
import {
  PLAYBACK_HEALTH_BATCH,
  _resetPlaybackHealthForTest,
  flushPlaybackHealth,
  recordPresignOutcome,
  recordQueueRestoreFailure,
  recordQueueSaveFailure,
  recordQueueStateRejected,
  type QueueRestoreStage,
} from '@features/playback/domain/playbackHealth';
import { playbackService } from '@features/playback/native/playbackService';

import { installPlaybackPort } from '@features/playback/ports/activePlaybackPort';
import type { PlaybackPort } from '@features/playback/ports/playbackPort';

import {
  previewTrack,
  libraryTrackWithId,
  cdnAudioUrls,
} from '@features/playback/__tests__/fixtures';
import {
  createMemoryPlaybackPort,
  type MemoryPlaybackSeed,
} from '@features/playback/__tests__/memoryPlaybackPort';

type AppStateChangeHandler = (state: string) => void;

jest.mock('react-native/Libraries/AppState/AppState', () => {
  const listeners: AppStateChangeHandler[] = [];
  return {
    default: {
      currentState: 'active',
      isAvailable: true,
      addEventListener: jest.fn((type: string, handler: AppStateChangeHandler) => {
        if (type === 'change') listeners.push(handler);
        return { remove: jest.fn() };
      }),
    },
    __listeners: listeners,
  };
});

jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: {
    auth: {
      getSession: jest
        .fn()
        .mockResolvedValue({ data: { session: { access_token: 'tok' } }, error: null }),
    },
  },
}));

const player = TrackPlayer as unknown as { getQueue: jest.Mock };
const { __player } = jest.requireMock('react-native-track-player') as {
  __player: { calls: (method: string) => unknown[][] };
};
let restorePort: (() => void) | null = null;

function installMemoryPort(
  seed: MemoryPlaybackSeed = {},
  override: Partial<PlaybackPort> = {},
): PlaybackPort {
  restorePort?.();
  const fake = { ...createMemoryPlaybackPort(seed), ...override };
  restorePort = installPlaybackPort(fake);
  return fake;
}

afterEach(() => {
  restorePort?.();
  restorePort = null;
});
const recordEventMock = recordEvent as jest.MockedFunction<typeof recordEvent>;

function appStateListeners(): AppStateChangeHandler[] {
  return (
    jest.requireMock('react-native/Libraries/AppState/AppState') as {
      __listeners: AppStateChangeHandler[];
    }
  ).__listeners;
}

function sentPayloads(): Record<string, unknown>[] {
  return recordEventMock.mock.calls.map(([event]) => {
    expect(event.type).toBe('playback_health');
    return event.payload ?? {};
  });
}

beforeEach(() => {
  _resetPlaybackHealthForTest();
  forgetAllSwaps();
  useQueueStore.getState().clearQueue();
  recordEventMock.mockReset().mockResolvedValue(undefined);
  installMemoryPort({ audioUrls: cdnAudioUrls(['t0', 't1', 't2', 't3']) });
  player.getQueue.mockResolvedValue([]);
  jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  jest.restoreAllMocks();
});

describe('playback health metric', () => {
  it('records a failed prefetch by stage', async () => {
    installMemoryPort({}, { fetchAudioUrls: () => Promise.reject(new Error('network down')) });
    useQueueStore
      .getState()
      .loadQueue([libraryTrackWithId('t0'), libraryTrackWithId('t1')], 0, null);

    await prefetchNext(0);
    flushPlaybackHealth();

    expect(sentPayloads()).toEqual([expect.objectContaining({ prefetch_failed_resolve: 1 })]);
    expect(sentPayloads()[0]).toMatchObject({ prefetch_ok: 0 });
  });

  it('records a successful prefetch', async () => {
    const [active, next] = [libraryTrackWithId('t0'), libraryTrackWithId('t1')];
    useQueueStore.getState().loadQueue([active, next], 0, null);
    player.getQueue.mockResolvedValue([{ id: trackKey(active) }, { id: trackKey(next) }]);

    await prefetchNext(0);
    flushPlaybackHealth();

    expect(sentPayloads()).toEqual([
      {
        prefetch_ok: 1,
        prefetch_failed_resolve: 0,
        prefetch_failed_download: 0,
        prefetch_failed_swap: 0,
        presign_ok: 0,
        presign_failed: 0,
        queue_rebuild_natural: 0,
        queue_rebuild_play_order: 0,
        queue_rebuild_exhausted: 0,
        playback_failed_network: 0,
        playback_failed_auth: 0,
        playback_failed_not_found: 0,
        playback_failed_decode: 0,
        playback_failed_queue_out_of_sync: 0,
        playback_failed_queue_update_failed: 0,
        playback_failed_unknown: 0,
        audio_recovery_failed: 0,
        queue_save_failed: 0,
        queue_restore_failed_fetch: 0,
        queue_restore_failed_placeholder: 0,
        queue_restore_failed_tracks: 0,
        queue_restore_failed_rebuild: 0,
        queue_restore_failed_native: 0,
        queue_state_rejected: 0,
      },
    ]);
  });

  it('records presign success and failure from queue loads', async () => {
    const tracks = [libraryTrackWithId('t0'), libraryTrackWithId('t1')];
    useQueueStore.getState().loadQueue(tracks, 0, null);

    await loadNativeQueue(tracks, 0, { autoplay: false });
    installMemoryPort({}, { fetchAudioUrls: () => Promise.reject(new Error('presign 503')) });
    await loadNativeQueue(tracks, 0, { autoplay: false });
    flushPlaybackHealth();

    expect(sentPayloads()).toEqual([expect.objectContaining({ presign_ok: 1, presign_failed: 1 })]);
  });

  it('sends one aggregate event per batch, not one per outcome', () => {
    for (let i = 0; i < PLAYBACK_HEALTH_BATCH * 2 + 3; i++) recordPresignOutcome(i % 5 !== 0);

    const payloads = sentPayloads();
    expect(payloads).toHaveLength(2);
    expect(payloads[0]).toMatchObject({ presign_ok: 20, presign_failed: 5 });
  });

  it('flushes the partial batch when the app goes to the background', () => {
    recordPresignOutcome(false);
    expect(recordEventMock).not.toHaveBeenCalled();

    for (const listener of appStateListeners()) listener('background');

    expect(sentPayloads()).toEqual([expect.objectContaining({ presign_failed: 1 })]);
  });

  it('sends nothing when there is nothing to report, and swallows a failed send', async () => {
    flushPlaybackHealth();
    expect(recordEventMock).not.toHaveBeenCalled();

    recordEventMock.mockRejectedValueOnce(new Error('offline'));
    recordPresignOutcome(true);
    expect(() => flushPlaybackHealth()).not.toThrow();
    await Promise.resolve();
    flushPlaybackHealth();
    expect(recordEventMock).toHaveBeenCalledTimes(1);
  });
});

describe('playback health metric — user-visible failures', () => {
  type PlaybackErrorHandler = (data: { code: string; message: string }) => void;

  async function firePlaybackError(code: string, message: string): Promise<void> {
    await playbackService();
    const registration = __player
      .calls('addEventListener')
      .find(([event]) => event === Event.PlaybackError);
    if (!registration) throw new Error('no PlaybackError listener was registered');
    (registration[1] as PlaybackErrorHandler)({ code, message });
    await new Promise((resolve) => setImmediate(resolve));
  }

  function nativeError(code: string): Error {
    return Object.assign(new Error(`native ${code}`), { code });
  }

  it('records a native playback error under its classified kind', async () => {
    useQueueStore.getState().loadQueue([previewTrack()], 0, null);

    await firePlaybackError('android-io-bad-http-status', 'Response code: 403');
    flushPlaybackHealth();

    expect(sentPayloads()).toEqual([expect.objectContaining({ playback_failed_auth: 1 })]);
  });

  it('records a playback error no track can be attributed to', async () => {
    await firePlaybackError('android-decoding-failed', 'Source error');
    flushPlaybackHealth();

    expect(useQueueStore.getState().currentTrack()).toBeNull();
    expect(sentPayloads()).toEqual([expect.objectContaining({ playback_failed_decode: 1 })]);
  });

  it('records a failed native queue mutation by its permanent or transient classification', () => {
    const key = trackKey(previewTrack());

    reportQueueFailure(key, 'skipNext', nativeError('no_current_item'));
    reportQueueFailure(null, 'signOutReset', new Error('bridge is busy'));
    flushPlaybackHealth();

    expect(sentPayloads()).toEqual([
      expect.objectContaining({
        playback_failed_queue_out_of_sync: 1,
        playback_failed_queue_update_failed: 1,
      }),
    ]);
  });

  it('counts a native error whose audio recovery request fails', async () => {
    const { __http } = jest.requireActual('@testing/doubles/fetch.js');
    __http.replyAll({ status: 500 });
    useQueueStore.getState().loadQueue([libraryTrackWithId('trk-1')], 0, null);

    await firePlaybackError('android-io-bad-http-status', 'Response code: 403');
    flushPlaybackHealth();

    expect(sentPayloads()).toEqual([
      expect.objectContaining({ playback_failed_auth: 1, audio_recovery_failed: 1 }),
    ]);
  });
});

describe('queue save and restore counters', () => {
  it('counts a save failure and a rejected state', () => {
    recordQueueSaveFailure();
    recordQueueStateRejected();
    flushPlaybackHealth();

    expect(sentPayloads()).toEqual([
      expect.objectContaining({ queue_save_failed: 1, queue_state_rejected: 1 }),
    ]);
  });

  it.each<QueueRestoreStage>(['fetch', 'placeholder', 'tracks', 'rebuild', 'native'])(
    'counts a restore failure at the %s stage',
    (stage) => {
      recordQueueRestoreFailure(stage);
      flushPlaybackHealth();

      expect(sentPayloads()).toEqual([
        expect.objectContaining({ [`queue_restore_failed_${stage}`]: 1 }),
      ]);
    },
  );
});
