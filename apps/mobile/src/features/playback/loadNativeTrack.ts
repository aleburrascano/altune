import TrackPlayer, { type AddTrack } from 'react-native-track-player';

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

function toNativeTrack(track: PlaybackTrack, headers: Record<string, string>): AddTrack {
  const artwork = track.artworkUrl ?? '';
  if (track.source.kind === 'preview') {
    return { url: track.source.previewUrl, title: track.title, artist: track.artist, artwork };
  }
  return {
    url: audioStreamUrl(track.source.trackId),
    title: track.title,
    artist: track.artist,
    artwork,
    headers,
  };
}

export async function loadNativeTrack(
  track: PlaybackTrack,
  options: LoadNativeTrackOptions = {},
): Promise<void> {
  const { autoplay = true, startPositionMs = 0 } = options;

  await ensurePlayerSetup();
  await TrackPlayer.reset();
  const headers = track.source.kind === 'library' ? await audioRequestHeaders() : {};
  await TrackPlayer.add(toNativeTrack(track, headers));

  if (startPositionMs > 0) {
    await TrackPlayer.seekTo(startPositionMs / 1000);
  }
  if (autoplay) {
    await TrackPlayer.play();
  }
}

// AIDEV-NOTE: Loads the whole ordered queue into the native player in one pass
// so TrackPlayer prefetches the next track and transitions are gapless — the
// fix for the "not playing" flash + slow switch that single-track reset+load
// caused. The native queue mirrors play order, so its index == store
// currentIndex. Auth headers are fetched once and reused across library items.
export async function loadNativeQueue(
  tracks: readonly PlaybackTrack[],
  startIndex: number,
  options: LoadNativeTrackOptions = {},
): Promise<void> {
  const { autoplay = true, startPositionMs = 0 } = options;

  await ensurePlayerSetup();
  await TrackPlayer.reset();
  if (tracks.length === 0) return;

  const needsAuth = tracks.some((t) => t.source.kind === 'library');
  const headers = needsAuth ? await audioRequestHeaders() : {};
  await TrackPlayer.add(tracks.map((t) => toNativeTrack(t, headers)));

  const idx = Math.max(0, Math.min(startIndex, tracks.length - 1));
  if (idx > 0) await TrackPlayer.skip(idx);
  if (startPositionMs > 0) await TrackPlayer.seekTo(startPositionMs / 1000);
  if (autoplay) await TrackPlayer.play();
}
