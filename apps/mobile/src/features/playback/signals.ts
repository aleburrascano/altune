/**
 * signals — pure helpers for behavioral playback telemetry.
 *
 * Kept pure (the state.ts pattern) so the listen-threshold and payload shaping
 * are unit-testable without a native player. The effect-driven emission lives in
 * usePlaybackSignals.
 */

import type { PlaybackTrack, QueueSource } from '@shared/playback/types';

// Kim WSDM 2014: a play becomes a satisfaction signal once the listen passes a
// threshold — 30s OR 50% of the track, whichever comes first. Below it the play
// is just a sample, not evidence the user wanted this result.
export const LISTEN_THRESHOLD_MS = 30000;

export function listenThresholdMs(durationMs: number): number {
  if (durationMs > 0) return Math.min(LISTEN_THRESHOLD_MS, durationMs * 0.5);
  return LISTEN_THRESHOLD_MS;
}

export function hasCrossedListenThreshold(positionMs: number, durationMs: number): boolean {
  return positionMs >= listenThresholdMs(durationMs);
}

export type TrackEventPayload = {
  title: string;
  artist: string;
  source_kind: string;
  track_id: string | null;
  surface: string | null;
  result_signature: string | null;
  dwell_ms?: number;
};

// trackKey is a stable identity for change detection — which library track or
// preview is playing, independent of object identity across renders.
export function trackKey(track: PlaybackTrack): string {
  const src =
    track.source.kind === 'library'
      ? `lib:${track.source.trackId}`
      : `prev:${track.source.previewUrl}`;
  return `${src}|${track.title}`;
}

export function buildTrackPayload(
  track: PlaybackTrack,
  queueSource: QueueSource | null,
  dwellMs?: number,
): TrackEventPayload {
  const payload: TrackEventPayload = {
    title: track.title,
    artist: track.artist,
    source_kind: track.source.kind,
    track_id: track.source.kind === 'library' ? track.source.trackId : null,
    surface: queueSource?.kind ?? null,
    result_signature: track.resultSignature ?? null,
  };
  if (dwellMs !== undefined) payload.dwell_ms = Math.round(dwellMs);
  return payload;
}
