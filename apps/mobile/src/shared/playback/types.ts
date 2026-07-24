import type { FeaturedArtist } from '@shared/api-client/types';

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
  readonly featuredArtists?: readonly FeaturedArtist[] | undefined;
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

export type RepeatMode = 'off' | 'all' | 'one';

export type QueueSource =
  | { readonly kind: 'playlist'; readonly playlistId: string; readonly name: string }
  | { readonly kind: 'library' }
  | { readonly kind: 'search'; readonly query: string };
