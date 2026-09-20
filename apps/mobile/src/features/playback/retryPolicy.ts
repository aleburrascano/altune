import type { PlaybackErrorKind } from '@shared/playback/types';

const UNPLAYABLE_KINDS: ReadonlySet<PlaybackErrorKind> = new Set<PlaybackErrorKind>([
  'not_found',
  'decode',
]);

export function canRetryPlaybackError(kind: PlaybackErrorKind | null): boolean {
  return kind === null || !UNPLAYABLE_KINDS.has(kind);
}
