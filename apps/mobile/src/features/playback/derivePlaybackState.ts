import type { PlaybackState, PlaybackTrack } from '@shared/playback/types';

export interface DerivePlaybackStateInput {
  track: PlaybackTrack | null;
  errorMessage: string | null;
  isBuffering: boolean;
  isEnded: boolean;
  isPlaying: boolean;
  positionMs: number;
  durationMs: number;
}

const IDLE: PlaybackState = {
  status: 'idle',
  track: null,
  positionMs: 0,
  durationMs: 0,
  errorMessage: null,
};

export function derivePlaybackState(input: DerivePlaybackStateInput): PlaybackState {
  const { track, errorMessage, isBuffering, isEnded, isPlaying, positionMs, durationMs } = input;

  if (!track) return IDLE;
  if (errorMessage) return { status: 'error', track, positionMs: 0, durationMs: 0, errorMessage };
  if (isBuffering) return { status: 'loading', track, positionMs, durationMs, errorMessage: null };
  if (isEnded)
    return { status: 'ended', track, positionMs: durationMs, durationMs, errorMessage: null };

  return {
    status: isPlaying ? 'playing' : 'paused',
    track,
    positionMs,
    durationMs,
    errorMessage: null,
  };
}
