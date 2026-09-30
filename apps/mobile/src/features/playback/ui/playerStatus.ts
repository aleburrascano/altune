import type { PlaybackStatus, PlaybackTrack } from '@shared/playback/types';

export function isPreviewTrack(track: PlaybackTrack) {
  return track.source.kind === 'preview';
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
