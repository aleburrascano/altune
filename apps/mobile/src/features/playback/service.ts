import TrackPlayer, { Event, type RemoteDuckEvent } from 'react-native-track-player';

import { RESTART_THRESHOLD_MS } from '@shared/playback/constants';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import { registerAudioCacheInvalidator } from '@shared/acquisition/audioCacheInvalidation';
import { recoverAudio } from '@shared/api-client/audio';
import { evictCached, prefetchNext, repairActiveToStreaming, wasSwappedToLocal } from './audioPrefetch';
import { refreshUpcomingPresign } from './loadNativeTrack';
import { withNativeQueue } from './nativeQueueLock';
import { shouldApplyActiveIndex } from './nativeSyncGuard';
import { reportPlaybackError } from './playbackErrorStore';

const RESTART_THRESHOLD_SECONDS = RESTART_THRESHOLD_MS / 1000;

async function activeTrackKey(): Promise<string | null> {
  const active = await TrackPlayer.getActiveTrack().catch(() => undefined);
  return typeof active?.id === 'string' ? active.id : null;
}

function queueTrackByKey(key: string): PlaybackTrack | null {
  const s = useQueueStore.getState();
  return orderedQueueTracks(s).find((t) => trackKey(t) === key) ?? null;
}

async function handlePlaybackError(message: string): Promise<void> {
  const key = await activeTrackKey();
  const failed = key !== null ? queueTrackByKey(key) : useQueueStore.getState().currentTrack();
  const failedKey = key ?? (failed ? trackKey(failed) : null);
  if (failedKey !== null) reportPlaybackError(failedKey, message || 'Playback failed');

  if (!failed || failed.source.kind !== 'library') return;
  if (wasSwappedToLocal(failed.source.trackId)) {
    await repairActiveToStreaming(failed);
    return;
  }
  await recoverAudio(failed.source.trackId).catch(() => {});
}

// Mirror an audio interruption (e.g. a phone call) onto the player. Pause when it
// begins, resume when it ends. iOS 18's native auto-resume after a call is
// unreliable even with autoHandleInterruptions, so this play() acts as the resume;
// both calls are idempotent, so they reinforce rather than fight one another. A
// permanent focus loss means another app took over — stay paused.
function handleRemoteDuck(data: RemoteDuckEvent): void {
  if (data.permanent) return;
  if (data.paused) {
    void TrackPlayer.pause();
    return;
  }
  void TrackPlayer.play();
}

export async function playbackService() {
  registerAudioCacheInvalidator(evictCached);

  TrackPlayer.addEventListener(Event.RemoteDuck, handleRemoteDuck);

  TrackPlayer.addEventListener(Event.RemotePause, () => {
    void TrackPlayer.pause();
  });
  TrackPlayer.addEventListener(Event.RemotePlay, () => {
    void TrackPlayer.play();
  });
  TrackPlayer.addEventListener(Event.RemoteNext, () => {
    void withNativeQueue(() => TrackPlayer.skipToNext()).catch(() => {});
  });
  TrackPlayer.addEventListener(Event.RemotePrevious, () => {
    void (async () => {
      const { position } = await TrackPlayer.getProgress();
      if (position > RESTART_THRESHOLD_SECONDS) {
        await TrackPlayer.seekTo(0);
        return;
      }
      await withNativeQueue(() => TrackPlayer.skipToPrevious()).catch(() => {});
    })();
  });
  TrackPlayer.addEventListener(Event.RemoteSeek, (data) => {
    void TrackPlayer.seekTo(data.position);
  });

  TrackPlayer.addEventListener(Event.PlaybackError, (data) => {
    void handlePlaybackError(data.message);
  });

  TrackPlayer.addEventListener(Event.PlaybackActiveTrackChanged, (data) => {
    if (typeof data.index !== 'number') return;
    if (!shouldApplyActiveIndex(data.index)) return;
    const key = typeof data.track?.id === 'string' ? data.track.id : undefined;
    useQueueStore.getState().syncCurrentIndex(data.index, key);
    const idx = useQueueStore.getState().currentIndex;
    void prefetchNext(idx);
    void refreshUpcomingPresign(idx);
  });
}
