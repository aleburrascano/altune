import TrackPlayer from 'react-native-track-player';

import { audioRequestHeaders, audioStreamUrl } from './api/audio';
import { ensurePlayerSetup } from './initPlayer';
import type { PlaybackTrack } from '@shared/playback/types';

export interface LoadNativeTrackOptions {
  // When false, the track is loaded but not started — used to resume a queue
  // paused at a saved position so the user presses play to continue.
  autoplay?: boolean;
  // Seek to this offset (ms) after loading. 0 starts from the top.
  startPositionMs?: number;
}

export async function loadNativeTrack(
  track: PlaybackTrack,
  options: LoadNativeTrackOptions = {},
): Promise<void> {
  const { autoplay = true, startPositionMs = 0 } = options;

  await ensurePlayerSetup();
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

  if (startPositionMs > 0) {
    await TrackPlayer.seekTo(startPositionMs / 1000);
  }
  if (autoplay) {
    await TrackPlayer.play();
  }
}
