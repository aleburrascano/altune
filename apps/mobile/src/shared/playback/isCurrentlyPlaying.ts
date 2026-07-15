import type { PlaybackContextValue, PlaybackSource } from './types';

export function isCurrentlyPlaying(
  playback: Pick<PlaybackContextValue, 'status' | 'track'>,
  source: PlaybackSource,
): boolean {
  // Treat a buffering/loading track as "active" too. During an auto-advance the
  // native player passes through loading before it reports playing; gating only
  // on 'playing' makes the blue highlight blank out for the whole transition,
  // then snap onto the new row. Following loading keeps the highlight on the
  // active track continuously (optimistic), instead of vanishing mid-switch.
  const isActive = playback.status === 'playing' || playback.status === 'loading';
  if (!isActive || !playback.track) return false;
  if (source.kind === 'library') {
    return playback.track.source.kind === 'library' && playback.track.source.trackId === source.trackId;
  }
  return playback.track.source.kind === 'preview' && playback.track.source.previewUrl === source.previewUrl;
}
