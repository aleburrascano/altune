export type PlaybackStatus = 'idle' | 'loading' | 'playing' | 'paused' | 'ended' | 'error';

export type PlaybackSource =
  | { readonly kind: 'library'; readonly trackId: string }
  | { readonly kind: 'preview'; readonly previewUrl: string };

export interface PlaybackTrack {
  readonly source: PlaybackSource;
  readonly title: string;
  readonly artist: string;
  readonly artworkUrl: string | null;
  readonly durationSeconds?: number | undefined;
  // Discovery provenance, present only when the track was queued from a search
  // result. Carried onto play/skip/completed events so behavioral satisfaction
  // joins back to the search and the result_signature it scores.
  readonly searchId?: string | undefined;
  readonly resultSignature?: string | undefined;
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
  pause(): void;
  resume(): void;
  seekTo(positionMs: number): void;
  stop(): void;
  retry(): void;
}

export type PlaybackContextValue = PlaybackState & PlaybackControls;

export type RepeatMode = 'off' | 'all' | 'one';

export type QueueSource =
  | { readonly kind: 'playlist'; readonly playlistId: string; readonly name: string }
  | { readonly kind: 'library' }
  | { readonly kind: 'search'; readonly query: string };
