import type { PlaybackTrack, QueueSource } from '@shared/playback/types';

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
  result_signature?: string;
  dwell_ms?: number;
};

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
  };
  if (track.resultSignature != null) payload.result_signature = track.resultSignature;
  if (dwellMs !== undefined) payload.dwell_ms = Math.round(dwellMs);
  return payload;
}
