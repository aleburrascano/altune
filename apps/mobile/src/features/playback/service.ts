import TrackPlayer, { Event, State } from 'react-native-track-player';

import { useQueueStore } from '@shared/playback/queueStore';

import { loadNativeTrack } from './loadNativeTrack';

export async function playbackService() {
  TrackPlayer.addEventListener(Event.RemotePause, () => {
    void TrackPlayer.pause();
  });
  TrackPlayer.addEventListener(Event.RemotePlay, () => {
    void TrackPlayer.play();
  });
  TrackPlayer.addEventListener(Event.RemoteNext, () => {
    const nextTrack = useQueueStore.getState().skipToNext();
    if (nextTrack) void loadNativeTrack(nextTrack);
  });
  TrackPlayer.addEventListener(Event.RemotePrevious, () => {
    const prevTrack = useQueueStore.getState().skipToPrevious();
    if (prevTrack) void loadNativeTrack(prevTrack);
  });
  TrackPlayer.addEventListener(Event.RemoteSeek, (data) => {
    void TrackPlayer.seekTo(data.position);
  });

  TrackPlayer.addEventListener(Event.PlaybackState, (data) => {
    if (data.state !== State.Ended) return;
    const store = useQueueStore.getState();
    if (store.repeatMode === 'one') {
      void TrackPlayer.seekTo(0).then(() => TrackPlayer.play());
      return;
    }
    const nextTrack = store.skipToNext();
    if (nextTrack) void loadNativeTrack(nextTrack);
  });
}
