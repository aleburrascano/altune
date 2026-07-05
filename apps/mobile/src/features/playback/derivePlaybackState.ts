import type { PlaybackState, PlaybackTrack } from '@shared/playback/types';

export interface DerivePlaybackStateInput {
  track: PlaybackTrack | null;
  errorMessage: string | null;
  isBuffering: boolean;
  isEnded: boolean;
  isPlaying: boolean;
  // Best-known display position (resume seed before native progress is live,
  // otherwise the live position) and duration.
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

// Pure derivation of the public PlaybackState from raw player signals. Extracted
// from the provider so the position-handling rules are unit-testable.
//
// AIDEV-NOTE: The buffering branch preserves positionMs (does NOT reset to 0).
// Priming the native queue on resume enters Buffering while the stream loads; a
// hardcoded 0 here made the scrubber slam to the start and then jump back to the
// seeked position — the resume "flicker". Holding the computed position keeps it
// steady: it's the resume seed before live progress arrives, or the real live
// position for a mid-playback stall.
export function derivePlaybackState(input: DerivePlaybackStateInput): PlaybackState {
  const { track, errorMessage, isBuffering, isEnded, isPlaying, positionMs, durationMs } = input;

  if (!track) return IDLE;
  if (errorMessage) return { status: 'error', track, positionMs: 0, durationMs: 0, errorMessage };
  if (isBuffering) return { status: 'loading', track, positionMs, durationMs, errorMessage: null };
  if (isEnded) return { status: 'ended', track, positionMs: durationMs, durationMs, errorMessage: null };

  return {
    status: isPlaying ? 'playing' : 'paused',
    track,
    positionMs,
    durationMs,
    errorMessage: null,
  };
}
