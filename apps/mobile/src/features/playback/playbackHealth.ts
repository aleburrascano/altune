import { AppState } from 'react-native';

import { recordEvent } from '@shared/telemetry/recordEvent';

// Prefetch and presign fall back to streaming silently, so their health degrades without any
// user-visible error. This tallies outcomes and reports them as one aggregate `playback_health`
// event (not one per track), from which a success rate per client batch can be computed.

// Where a prefetch failed: resolving the signed URL, downloading the file, or installing it
// (cache lookup, native swap, eviction).
export type PrefetchFailureStage = 'resolve' | 'download' | 'swap';

// Outcomes per reported batch; the batch also flushes when the app goes to the background.
export const PLAYBACK_HEALTH_BATCH = 25;

type Tally = {
  prefetch_ok: number;
  prefetch_failed_resolve: number;
  prefetch_failed_download: number;
  prefetch_failed_swap: number;
  presign_ok: number;
  presign_failed: number;
};

const emptyTally = (): Tally => ({
  prefetch_ok: 0,
  prefetch_failed_resolve: 0,
  prefetch_failed_download: 0,
  prefetch_failed_swap: 0,
  presign_ok: 0,
  presign_failed: 0,
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
