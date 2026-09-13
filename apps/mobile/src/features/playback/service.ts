import TrackPlayer, { Event, type RemoteDuckEvent } from 'react-native-track-player';

import { RESTART_THRESHOLD_MS } from '@shared/playback/constants';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import { registerAudioCacheInvalidator } from '@shared/acquisition/audioCacheInvalidation';
import { recoverAudio } from '@shared/api-client/audio';
import { evictCached, prefetchNext, repairActiveToStreaming, wasSwappedToLocal } from './audioPrefetch';
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

// Whether playback was playing when the current audio interruption (a phone
// call, Siri, another app's audio) began. Null when no interruption is open.
let playingBeforeInterruption: boolean | null = null;

// RemoteDuck payloads (RNTP v4): iOS sends {paused: true} when an interruption
// begins, {paused: false} when it ends and the system allows resuming, and
// {paused: true, permanent: true} when it ends without permission to resume.
// Android maps audio focus onto the same shape. `permanent` is omitted, not
// false, on the non-permanent payloads.
//
// autoHandleInterruptions already calls play() natively on an allowed resume,
// but that alone has left music paused after calls on iOS, so this resumes
// explicitly. play() is idempotent, so the two never fight. playWhenReady is
// the snapshot: it holds the user's intent and a system interruption does not
// clear it, so a track the user had paused stays paused.
async function handleRemoteDuck({ paused, permanent }: RemoteDuckEvent): Promise<void> {
  if (permanent === true) {
    playingBeforeInterruption = null;
    return;
  }
  if (paused) {
    if (playingBeforeInterruption === null) {
      playingBeforeInterruption = await TrackPlayer.getPlayWhenReady().catch(() => false);
    }
    return;
  }
  const shouldResume = playingBeforeInterruption === true;
  playingBeforeInterruption = null;
  if (shouldResume) await TrackPlayer.play().catch(() => {});
}

export async function playbackService() {
  playingBeforeInterruption = null;
  registerAudioCacheInvalidator(evictCached);

  TrackPlayer.addEventListener(Event.RemotePause, () => {
    playingBeforeInterruption = null;
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

  TrackPlayer.addEventListener(Event.RemoteDuck, (data) => {
    void handleRemoteDuck(data);
  });

  TrackPlayer.addEventListener(Event.PlaybackError, (data) => {
    void handlePlaybackError(data.message);
  });

  TrackPlayer.addEventListener(Event.PlaybackActiveTrackChanged, (data) => {
    if (typeof data.index !== 'number') return;
    if (!shouldApplyActiveIndex(data.index)) return;
    const key = typeof data.track?.id === 'string' ? data.track.id : undefined;
    useQueueStore.getState().syncCurrentIndex(data.index, key);
    void prefetchNext(useQueueStore.getState().currentIndex);
  });
}
