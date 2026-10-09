import type { PlaybackErrorKind, PlaybackStatus } from '@shared/playback/playbackContextValue';
import type { PlaybackTrack } from '@shared/playback/playbackTrack';

const UNPLAYABLE_KINDS: ReadonlySet<PlaybackErrorKind> = new Set<PlaybackErrorKind>([
  'not_found',
  'decode',
]);

export function canRetryPlaybackError(kind: PlaybackErrorKind | null): boolean {
  return kind === null || !UNPLAYABLE_KINDS.has(kind);
}

export function isPreviewTrack(track: PlaybackTrack) {
  return track.source.kind === 'preview';
}

export function libraryTrackId(track: PlaybackTrack): string | undefined {
  return track.source.kind === 'library' ? track.source.trackId : undefined;
}

export function endedLabel(isPreview: boolean) {
  return isPreview ? 'Preview ended' : 'Finished';
}

export function statusFlagsOf(status: PlaybackStatus) {
  return {
    isPlaying: status === 'playing',
    isEnded: status === 'ended',
    isError: status === 'error',
  };
}
