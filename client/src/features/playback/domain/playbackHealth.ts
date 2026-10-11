import { subscribeAppState } from '@shared/lifecycle/appState';
import type { PlaybackErrorKind } from '@shared/playback/playbackContextValue';
import { recordEvent } from '@shared/telemetry/recordEvent';

import { warnPlayback } from '@features/playback/domain/playbackError';

export type PrefetchFailureStage = 'resolve' | 'download' | 'swap';

export type QueueRestoreStage = 'fetch' | 'placeholder' | 'tracks' | 'rebuild' | 'native';

export type QueueRebuildRung = 'natural' | 'play_order' | 'exhausted';

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
  audio_recovery_failed: number;
  queue_save_failed: number;
  queue_restore_failed_fetch: number;
  queue_restore_failed_placeholder: number;
  queue_restore_failed_tracks: number;
  queue_restore_failed_rebuild: number;
  queue_restore_failed_native: number;
  queue_state_rejected: number;
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
  audio_recovery_failed: 0,
  queue_save_failed: 0,
  queue_restore_failed_fetch: 0,
  queue_restore_failed_placeholder: 0,
  queue_restore_failed_tracks: 0,
  queue_restore_failed_rebuild: 0,
  queue_restore_failed_native: 0,
  queue_state_rejected: 0,
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

export function recordPlaybackFailure(kind: PlaybackErrorKind): void {
  count(`playback_failed_${kind}`);
}

export function recordAudioRecoveryFailure(): void {
  count('audio_recovery_failed');
}

export function recordQueueSaveFailure(): void {
  count('queue_save_failed');
}

export function recordQueueRestoreFailure(stage: QueueRestoreStage): void {
  count(`queue_restore_failed_${stage}`);
}

export function recordQueueStateRejected(): void {
  count('queue_state_rejected');
}

export function flushPlaybackHealth(): void {
  if (outcomes === 0) return;
  const payload = tally;
  tally = emptyTally();
  outcomes = 0;
  recordEvent({ type: 'playback_health', payload }).catch((err: unknown) => {
    warnPlayback('failed to record playback health', {}, err);
  });
}

function ensureFlushOnBackground(): void {
  if (listening) return;
  listening = true;
  subscribeAppState((status) => {
    if (status === 'background') flushPlaybackHealth();
  });
}

export function _resetPlaybackHealthForTest(): void {
  tally = emptyTally();
  outcomes = 0;
}
