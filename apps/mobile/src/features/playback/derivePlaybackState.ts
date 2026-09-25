import type { PlaybackState, PlaybackTrack } from '@shared/playback/types';

import type { RedactedPlaybackFailure } from './redactPlaybackError';

export interface DerivePlaybackStateInput {
  track: PlaybackTrack | null;
  failure: RedactedPlaybackFailure | null;
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
  errorKind: null,
};

const NO_FAILURE = { errorMessage: null, errorKind: null } as const;

export function derivePlaybackState(input: DerivePlaybackStateInput): PlaybackState {
  const { track, failure, isBuffering, isEnded, isPlaying, positionMs, durationMs } = input;

  if (!track) return IDLE;
  if (failure) {
    return {
      status: 'error',
      track,
      positionMs: 0,
      durationMs: 0,
      errorMessage: failure.message,
      errorKind: failure.kind,
    };
  }
  if (isBuffering) return { status: 'loading', track, positionMs, durationMs, ...NO_FAILURE };
  if (isEnded)
    return { status: 'ended', track, positionMs: durationMs, durationMs, ...NO_FAILURE };

  return {
    status: isPlaying ? 'playing' : 'paused',
    track,
    positionMs,
    durationMs,
    ...NO_FAILURE,
  };
}
