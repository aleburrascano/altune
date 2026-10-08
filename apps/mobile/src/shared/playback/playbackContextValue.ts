import type { PlaybackTrack } from '@shared/playback/playbackTrack';

export type PlaybackStatus = 'idle' | 'loading' | 'playing' | 'paused' | 'ended' | 'error';

export type PlaybackErrorKind =
  | 'network'
  | 'auth'
  | 'not_found'
  | 'decode'
  | 'queue_out_of_sync'
  | 'queue_update_failed'
  | 'unknown';

export interface PlaybackState {
  readonly status: PlaybackStatus;
  readonly track: PlaybackTrack | null;
  readonly positionMs: number;
  readonly durationMs: number;
  readonly errorMessage: string | null;
  readonly errorKind: PlaybackErrorKind | null;
}

export interface PlaybackControls {
  play(track: PlaybackTrack): Promise<void>;
  startQueue(
    orderedTracks: readonly PlaybackTrack[],
    startIndex: number,
    options?: { autoplay?: boolean; startPositionMs?: number },
  ): Promise<void>;
  skipToQueueIndex(index: number): Promise<void>;
  reorderUpcoming(upcomingTracks: readonly PlaybackTrack[]): Promise<void>;
  appendToQueue(track: PlaybackTrack): Promise<void>;
  insertNext(track: PlaybackTrack, position: number): Promise<void>;
  skipNext(): Promise<void>;
  skipPrevious(): Promise<void>;
  removeQueueIndex(index: number): Promise<void>;
  pause(): void;
  resume(): void;
  seekTo(positionMs: number): void;
  setRate(rate: number): void;
  stop(): void;
  retry(): void;
}

export type PlaybackContextValue = PlaybackState & PlaybackControls;
