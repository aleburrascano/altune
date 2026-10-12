import { act } from '@testing-library/react-native';
import TrackPlayer, { Event } from 'react-native-track-player';

import { useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/playbackTrack';

import { signInTestUser } from '@testing/signInTestUser';

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
const { __http } = require('@testing/doubles/fetch.js');
const HEALTH_ROUTE = 'POST /v1/discovery/events';
const HTTP_ACCEPTED = 202;

function appStateListeners(): AppStateChangeHandler[] {
  return (
    jest.requireMock('react-native/Libraries/AppState/AppState') as {
      __listeners: AppStateChangeHandler[];
    }
  ).__listeners;
}

async function settleRequests(): Promise<void> {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

async function sentPayloads(): Promise<Record<string, unknown>[]> {
  await settleRequests();
  return (__http.requests as { path: string; body: string }[])
    .filter((request) => request.path === '/v1/discovery/events')
    .map((request) => {
      const event = JSON.parse(request.body) as { type: string; payload?: Record<string, unknown> };
      expect(event.type).toBe('playback_health');
      const counts = { ...event.payload };
      delete counts.session_id;
      return counts;
    });
}

beforeEach(async () => {
  await signInTestUser();
  _resetPlaybackHealthForTest();
  forgetAllSwaps();
  useQueueStore.getState().clearQueue();
  __http.reply(HEALTH_ROUTE, { status: HTTP_ACCEPTED });
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

    expect(await sentPayloads()).toEqual([expect.objectContaining({ prefetch_failed_resolve: 1 })]);
    expect((await sentPayloads())[0]).toMatchObject({ prefetch_ok: 0 });
  });

  it('records a successful prefetch', async () => {
    const [active, next] = [libraryTrackWithId('t0'), libraryTrackWithId('t1')];
    useQueueStore.getState().loadQueue([active, next], 0, null);
    player.getQueue.mockResolvedValue([{ id: trackKey(active) }, { id: trackKey(next) }]);

    await prefetchNext(0);
    flushPlaybackHealth();

    expect(await sentPayloads()).toEqual([
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

    expect(await sentPayloads()).toEqual([
      expect.objectContaining({ presign_ok: 1, presign_failed: 1 }),
    ]);
  });

  it('sends one aggregate event per batch, not one per outcome', async () => {
    for (let i = 0; i < PLAYBACK_HEALTH_BATCH * 2 + 3; i++) recordPresignOutcome(i % 5 !== 0);

    const payloads = await sentPayloads();
    expect(payloads).toHaveLength(2);
    expect(payloads[0]).toMatchObject({ presign_ok: 20, presign_failed: 5 });
  });

  it('flushes the partial batch when the app goes to the background', async () => {
    recordPresignOutcome(false);
    expect(await sentPayloads()).toEqual([]);

    for (const listener of appStateListeners()) listener('background');

    expect(await sentPayloads()).toEqual([expect.objectContaining({ presign_failed: 1 })]);
  });

  it('sends nothing when there is nothing to report, and swallows a failed send', async () => {
    flushPlaybackHealth();
    expect(await sentPayloads()).toEqual([]);

    __http.reset();
    __http.fail(HEALTH_ROUTE);
    recordPresignOutcome(true);
    expect(() => flushPlaybackHealth()).not.toThrow();
    await settleRequests();
    expect(__http.countFor(HEALTH_ROUTE)).toBe(1);
    flushPlaybackHealth();
    await settleRequests();
    expect(__http.countFor(HEALTH_ROUTE)).toBe(1);
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

    expect(await sentPayloads()).toEqual([expect.objectContaining({ playback_failed_auth: 1 })]);
  });

  it('records a playback error no track can be attributed to', async () => {
    await firePlaybackError('android-decoding-failed', 'Source error');
    flushPlaybackHealth();

    expect(useQueueStore.getState().currentTrack()).toBeNull();
    expect(await sentPayloads()).toEqual([expect.objectContaining({ playback_failed_decode: 1 })]);
  });

  it('records a failed native queue mutation by its permanent or transient classification', async () => {
    const key = trackKey(previewTrack());

    reportQueueFailure(key, 'skipNext', nativeError('no_current_item'));
    reportQueueFailure(null, 'signOutReset', new Error('bridge is busy'));
    flushPlaybackHealth();

    expect(await sentPayloads()).toEqual([
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

    expect(await sentPayloads()).toEqual([
      expect.objectContaining({ playback_failed_auth: 1, audio_recovery_failed: 1 }),
    ]);
  });
});

describe('queue save and restore counters', () => {
  it('counts a save failure and a rejected state', async () => {
    recordQueueSaveFailure();
    recordQueueStateRejected();
    flushPlaybackHealth();

    expect(await sentPayloads()).toEqual([
      expect.objectContaining({ queue_save_failed: 1, queue_state_rejected: 1 }),
    ]);
  });

  it.each<QueueRestoreStage>(['fetch', 'placeholder', 'tracks', 'rebuild', 'native'])(
    'counts a restore failure at the %s stage',
    async (stage) => {
      recordQueueRestoreFailure(stage);
      flushPlaybackHealth();

      expect(await sentPayloads()).toEqual([
        expect.objectContaining({ [`queue_restore_failed_${stage}`]: 1 }),
      ]);
    },
  );
});
