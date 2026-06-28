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
  /** Play a single track immediately (search/detail previews). Bypasses the queue. */
  play(track: PlaybackTrack): Promise<void>;
  /** Load an ordered track list into the native queue and start at startIndex. */
  startQueue(
    orderedTracks: readonly PlaybackTrack[],
    startIndex: number,
    options?: { autoplay?: boolean; startPositionMs?: number },
  ): Promise<void>;
  /** Jump to an already-loaded queue position (instant — track is prefetched). */
  skipToQueueIndex(index: number): Promise<void>;
  /** Advance to the next queued track natively (gapless, no JS cold-load). */
  skipNext(): Promise<void>;
  /** Return to the previous queued track natively. */
  skipPrevious(): Promise<void>;
  /** Remove a queued track by its play-order position. */
  removeQueueIndex(index: number): Promise<void>;
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
