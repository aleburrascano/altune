import { AppState } from 'react-native';

import type { PlaybackErrorKind } from '@shared/playback/types';
import { recordEvent } from '@shared/telemetry/recordEvent';

// Prefetch and presign fall back to streaming silently, so their health degrades without any
// user-visible error. This tallies outcomes and reports them as one aggregate `playback_health`
// event (not one per track), from which a success rate per client batch can be computed.

// Where a prefetch failed: resolving the signed URL, downloading the file, or installing it
// (cache lookup, native swap, eviction).
export type PrefetchFailureStage = 'resolve' | 'download' | 'swap';

// Which rung of the queue-rebuild fallback ladder answered a resume: `natural` restored the
// saved queue whole, `play_order` is the degraded rung (the natural order behind shuffle is
// lost), `exhausted` restored no queue at all. One outcome per resume, so the share landing
// below `natural` is the signal — the degraded rungs look like an ordinary resume on screen.
export type QueueRebuildRung = 'natural' | 'play_order' | 'exhausted';

// Outcomes per reported batch; the batch also flushes when the app goes to the background.
export const PLAYBACK_HEALTH_BATCH = 25;

type Tally = {
  prefetch_ok: number;
  prefetch_failed_resolve: number;
  prefetch_failed_download: number;
  prefetch_failed_swap: number;
  presign_ok: number;
  presign_failed: number;
  queue_rebuild_natural: number;
  queue_rebuild_play_order: number;
  queue_rebuild_exhausted: number;
  playback_failed_network: number;
  playback_failed_auth: number;
  playback_failed_not_found: number;
  playback_failed_decode: number;
  playback_failed_queue_out_of_sync: number;
  playback_failed_queue_update_failed: number;
  playback_failed_unknown: number;
};

const emptyTally = (): Tally => ({
  prefetch_ok: 0,
  prefetch_failed_resolve: 0,
  prefetch_failed_download: 0,
  prefetch_failed_swap: 0,
  presign_ok: 0,
  presign_failed: 0,
  queue_rebuild_natural: 0,
  queue_rebuild_play_order: 0,
  queue_rebuild_exhausted: 0,
  playback_failed_network: 0,
  playback_failed_auth: 0,
  playback_failed_not_found: 0,
  playback_failed_decode: 0,
  playback_failed_queue_out_of_sync: 0,
  playback_failed_queue_update_failed: 0,
  playback_failed_unknown: 0,
});

let tally = emptyTally();
let outcomes = 0;
let listening = false;

function count(key: keyof Tally): void {
  ensureFlushOnBackground();
  tally[key] += 1;
  outcomes += 1;
  if (outcomes >= PLAYBACK_HEALTH_BATCH) flushPlaybackHealth();
}

export function recordPrefetchOutcome(outcome: 'ok' | PrefetchFailureStage): void {
  count(outcome === 'ok' ? 'prefetch_ok' : `prefetch_failed_${outcome}`);
}

export function recordPresignOutcome(ok: boolean): void {
  count(ok ? 'presign_ok' : 'presign_failed');
}

export function recordQueueRebuildOutcome(rung: QueueRebuildRung): void {
  count(`queue_rebuild_${rung}`);
}

// The inverse of the fallbacks above: a failure the user watched happen — a native PlaybackError
// or a native queue mutation that diverged — so nothing here degrades silently. Tallied all the
// same, because the prefetch and presign rates stay healthy right through a codec regression or
// a batch of bad signed URLs, and only these buckets would show it (#1744).
export function recordPlaybackFailure(kind: PlaybackErrorKind): void {
  count(`playback_failed_${kind}`);
}

// Best effort: a batch that fails to send is dropped, since a health sample is not worth an
// outbox slot the label-critical events need.
export function flushPlaybackHealth(): void {
  if (outcomes === 0) return;
  const payload = tally;
  tally = emptyTally();
  outcomes = 0;
  recordEvent({ type: 'playback_health', payload }).catch(() => undefined);
}

function ensureFlushOnBackground(): void {
  if (listening) return;
  listening = true;
  AppState.addEventListener('change', (status) => {
    if (status === 'background') flushPlaybackHealth();
  });
}

export function _resetPlaybackHealthForTest(): void {
  tally = emptyTally();
  outcomes = 0;
}
