export type PlaybackStatus = 'idle' | 'loading' | 'playing' | 'paused' | 'error';

export interface PlaybackTrack {
  readonly trackId: string;
  readonly title: string;
  readonly artist: string;
  readonly artworkUrl: string | null;
}

export interface PlaybackState {
  readonly status: PlaybackStatus;
  readonly track: PlaybackTrack | null;
  readonly positionMs: number;
  readonly durationMs: number;
  readonly errorMessage: string | null;
}

export interface PlaybackControls {
  play(track: PlaybackTrack): Promise<void>;
  pause(): Promise<void>;
  resume(): Promise<void>;
  seekTo(positionMs: number): Promise<void>;
  stop(): Promise<void>;
}

export type PlaybackContextValue = PlaybackState & PlaybackControls;
