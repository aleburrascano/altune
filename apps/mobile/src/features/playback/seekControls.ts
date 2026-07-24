import TrackPlayer from 'react-native-track-player';

export async function seekPreservingPlayback(seconds: number, wasPlaying: boolean): Promise<void> {
  await TrackPlayer.seekTo(seconds);
  if (wasPlaying) await TrackPlayer.play();
}
