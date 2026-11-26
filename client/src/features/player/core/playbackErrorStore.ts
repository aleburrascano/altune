import { create } from 'zustand';

import {
  type PlaybackErrorKind,
  type TrackKey,
  trackKey,
  type PlaybackTrack,
} from '@shared/playback';

import {
  classifyPlaybackFailure,
  redactPlaybackErrorMessage,
  warnPlayback,
} from '@features/player/domain/playbackError';
import { recordPlaybackFailure } from '@features/player/domain/playbackHealth';

interface PlaybackErrorState {
  key: TrackKey | null;
  kind: PlaybackErrorKind | null;
  message: string | null;
  report: (key: TrackKey, kind: PlaybackErrorKind, message: string) => void;
  clear: () => void;
}

export const usePlaybackErrorStore = create<PlaybackErrorState>((set) => ({
  key: null,
  kind: null,
  message: null,
  report: (key, kind, message) => set({ key, kind, message: redactPlaybackErrorMessage(message) }),
  clear: () => set({ key: null, kind: null, message: null }),
}));

export function reportPlaybackError(key: TrackKey, kind: PlaybackErrorKind, message: string): void {
  usePlaybackErrorStore.getState().report(key, kind, message);
}

function loadFailureMessage(err: unknown): string {
  return err instanceof Error ? err.message : 'Failed to load audio';
}

export function reportLoadFailure(
  track: PlaybackTrack,
  err: unknown,
  message: string = loadFailureMessage(err),
): void {
  const kind = classifyPlaybackFailure(err);
  warnPlayback('track load failed', { kind }, err);
  recordPlaybackFailure(kind);
  reportPlaybackError(trackKey(track), kind, message);
}

export function clearPlaybackError(): void {
  usePlaybackErrorStore.getState().clear();
}
