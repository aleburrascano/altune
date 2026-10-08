import type { PlaybackContextValue } from '@shared/playback/playbackContextValue';
import type { PlaybackSource } from '@shared/playback/playbackTrack';

export function isCurrentlyPlaying(
  playback: Pick<PlaybackContextValue, 'status' | 'track'>,
  source: PlaybackSource,
): boolean {
  const isActive = playback.status === 'playing' || playback.status === 'loading';
  if (!isActive || !playback.track) return false;
  if (source.kind === 'library') {
    return (
      playback.track.source.kind === 'library' && playback.track.source.trackId === source.trackId
    );
  }
  return (
    playback.track.source.kind === 'preview' &&
    playback.track.source.previewUrl === source.previewUrl
  );
}
