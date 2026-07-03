import TrackPlayer from 'react-native-track-player';

// AIDEV-NOTE: iOS RNTP (SwiftAudioEx) does not reliably re-emit State.Playing
// after a seekTo — the player settles into Ready/Paused and stays there until an
// explicit play(), so usePlaybackState goes stale (UI shows paused + 0:00) even
// though audio keeps going. Re-asserting play() resyncs the reported state to
// reality. Guarded on wasPlaying so scrubbing a *paused* track never starts
// playback. play() on an already-playing track is a native no-op.
export async function seekPreservingPlayback(seconds: number, wasPlaying: boolean): Promise<void> {
  await TrackPlayer.seekTo(seconds);
  if (wasPlaying) await TrackPlayer.play();
}
