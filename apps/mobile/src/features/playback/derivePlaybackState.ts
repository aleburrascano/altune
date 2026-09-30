import type { PlaybackState, PlaybackTrack } from '@shared/playback/types';

import type { RedactedPlaybackFailure } from './redactPlaybackError';

export type PlayerPhase = 'loading' | 'playing' | 'paused' | 'ended';

export interface DerivePlaybackStateInput {
  track: PlaybackTrack | null;
  failure: RedactedPlaybackFailure | null;
  phase: PlayerPhase;
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
  const { track, failure, phase, positionMs, durationMs } = input;

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
  if (phase === 'loading')
    return { status: 'loading', track, positionMs, durationMs, ...NO_FAILURE };
  if (phase === 'ended')
    return { status: 'ended', track, positionMs: durationMs, durationMs, ...NO_FAILURE };

  return {
    status: phase,
    track,
    positionMs,
    durationMs,
    ...NO_FAILURE,
  };
}
