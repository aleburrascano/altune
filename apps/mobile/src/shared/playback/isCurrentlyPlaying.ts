import type { PlaybackContextValue, PlaybackSource } from './types';

export function isCurrentlyPlaying(
  playback: Pick<PlaybackContextValue, 'status' | 'track'>,
  source: PlaybackSource,
): boolean {
  if (playback.status !== 'playing' || !playback.track) return false;
  if (source.kind === 'library') {
    return playback.track.source.kind === 'library' && playback.track.source.trackId === source.trackId;
  }
  return playback.track.source.kind === 'preview' && playback.track.source.previewUrl === source.previewUrl;
}
