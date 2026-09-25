import type { PlaylistId, TrackId } from '@shared/api-client/ids';
import type { FeaturedArtist } from '@shared/api-client/types';

export type PlaybackStatus = 'idle' | 'loading' | 'playing' | 'paused' | 'ended' | 'error';

/**
 * What kind of failure a playback error is, so callers branch on this, never on the message.
 * - `network`: no connection, a timeout, or a server-side (5xx/429) failure; may succeed later.
 * - `auth`: the stream request was refused (401/403), e.g. an expired signed URL or session.
 * - `not_found`: the audio no longer exists (404/410, missing file).
 * - `decode`: the audio arrived but cannot be parsed or decoded.
 * - `queue_out_of_sync` / `queue_update_failed`: a native queue mutation failed (permanent
 *   drift vs a transient failure), see `createNativePlaybackActions`.
 * - `unknown`: anything the native layer or loader does not let us tell apart.
 */
export type PlaybackErrorKind =
  | 'network'
  | 'auth'
  | 'not_found'
  | 'decode'
  | 'queue_out_of_sync'
  | 'queue_update_failed'
  | 'unknown';

export type PlaybackSource =
  | { readonly kind: 'library'; readonly trackId: TrackId }
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

export type RepeatMode = 'off' | 'all' | 'one';

export type QueueSource =
  | { readonly kind: 'playlist'; readonly playlistId: PlaylistId; readonly name: string }
  | { readonly kind: 'library' }
  | { readonly kind: 'search'; readonly query: string };
