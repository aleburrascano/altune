import { useMemo } from 'react';
import { create } from 'zustand';

import { type TrackKey, trackKey } from '@shared/playback/trackKey';
import type { PlaybackErrorKind, PlaybackTrack } from '@shared/playback/types';

import { classifyPlaybackFailure } from './classifyPlaybackError';
import { redactPlaybackErrorMessage, type RedactedPlaybackFailure } from './redactPlaybackError';

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
  reportPlaybackError(trackKey(track), classifyPlaybackFailure(err), message);
}

export function clearPlaybackError(): void {
  usePlaybackErrorStore.getState().clear();
}

export function usePlaybackErrorFor(key: TrackKey | null): RedactedPlaybackFailure | null {
  const kind = usePlaybackErrorStore((s) => (key != null && s.key === key ? s.kind : null));
  const message = usePlaybackErrorStore((s) => (key != null && s.key === key ? s.message : null));

  return useMemo(
    () => (kind !== null && message !== null ? { kind, message } : null),
    [kind, message],
  );
}
