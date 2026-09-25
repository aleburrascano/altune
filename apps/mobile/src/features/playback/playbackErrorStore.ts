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

/**
 * The one way a failed load is surfaced: classified, keyed to the track that failed.
 * `message` defaults to the rejection's own text, for callers with nothing better to show.
 */
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

/**
 * The failure a given track should show, kind included, or null when that track has none.
 * Selects the two fields separately and memoizes the pair: a selector building the object
 * itself returns a new reference on every render, which `useSyncExternalStore` rejects.
 */
export function usePlaybackErrorFor(key: TrackKey | null): RedactedPlaybackFailure | null {
  const kind = usePlaybackErrorStore((s) => (key != null && s.key === key ? s.kind : null));
  const message = usePlaybackErrorStore((s) => (key != null && s.key === key ? s.message : null));

  return useMemo(
    () => (kind !== null && message !== null ? { kind, message } : null),
    [kind, message],
  );
}
