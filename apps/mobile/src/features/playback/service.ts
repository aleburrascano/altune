import TrackPlayer, { Event } from 'react-native-track-player';

import { RESTART_THRESHOLD_MS } from '@shared/playback/constants';
import { useQueueStore } from '@shared/playback/queueStore';

import { recoverAudio } from '@shared/api-client/audio';
import { prefetchNext, repairActiveToStreaming, wasSwappedToLocal } from './audioPrefetch';
import { shouldApplyActiveIndex } from './nativeSyncGuard';

const RESTART_THRESHOLD_SECONDS = RESTART_THRESHOLD_MS / 1000;

export async function playbackService() {
  TrackPlayer.addEventListener(Event.RemotePause, () => {
    void TrackPlayer.pause();
  });
  TrackPlayer.addEventListener(Event.RemotePlay, () => {
    void TrackPlayer.play();
  });
  TrackPlayer.addEventListener(Event.RemoteNext, () => {
    void TrackPlayer.skipToNext();
  });
  TrackPlayer.addEventListener(Event.RemotePrevious, async () => {
    const { position } = await TrackPlayer.getProgress();
    if (position > RESTART_THRESHOLD_SECONDS) {
      await TrackPlayer.seekTo(0);
    } else {
      await TrackPlayer.skipToPrevious();
    }
  });
  TrackPlayer.addEventListener(Event.RemoteSeek, (data) => {
    void TrackPlayer.seekTo(data.position);
  });

  TrackPlayer.addEventListener(Event.PlaybackError, () => {
    const track = useQueueStore.getState().currentTrack();
    if (!track || track.source.kind !== 'library') return;
    if (wasSwappedToLocal(track.source.trackId)) {
      void repairActiveToStreaming(track);
      return;
    }
    void recoverAudio(track.source.trackId).catch(() => {});
  });

  TrackPlayer.addEventListener(Event.PlaybackActiveTrackChanged, (data) => {
    if (typeof data.index === 'number') {
      if (!shouldApplyActiveIndex(data.index)) return;
      useQueueStore.getState().syncCurrentIndex(data.index);
      console.log(`[audio-timing] track-transition index=${data.index} at=${Date.now()}`);
      void prefetchNext(data.index);
    }
  });
}
