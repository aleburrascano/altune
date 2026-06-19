import TrackPlayer, { Event } from 'react-native-track-player';

export async function playbackService() {
  TrackPlayer.addEventListener(Event.RemotePause, () => { void TrackPlayer.pause(); });
  TrackPlayer.addEventListener(Event.RemotePlay, () => { void TrackPlayer.play(); });
  TrackPlayer.addEventListener(Event.RemoteNext, () => {
    // Handled in PlaybackProvider via useQueueStore
  });
  TrackPlayer.addEventListener(Event.RemotePrevious, () => {
    // Handled in PlaybackProvider via useQueueStore
  });
  TrackPlayer.addEventListener(Event.RemoteSeek, (data) => {
    void TrackPlayer.seekTo(data.position);
  });
}
