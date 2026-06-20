import TrackPlayer from 'react-native-track-player';

import { audioRequestHeaders, audioStreamUrl } from './api/audio';
import type { PlaybackTrack } from '@shared/playback/types';

export async function loadNativeTrack(track: PlaybackTrack): Promise<void> {
  await TrackPlayer.reset();
  const artwork = track.artworkUrl ?? '';
  if (track.source.kind === 'preview') {
    await TrackPlayer.load({
      url: track.source.previewUrl,
      title: track.title,
      artist: track.artist,
      artwork,
    });
  } else {
    const headers = await audioRequestHeaders();
    await TrackPlayer.load({
      url: audioStreamUrl(track.source.trackId),
      title: track.title,
      artist: track.artist,
      artwork,
      headers,
    });
  }
  await TrackPlayer.play();
}
