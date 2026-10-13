import type { PlaybackErrorKind } from '@shared/playback/playbackContextValue';
import { HEALTH_BATCH, createHealthTally } from '@shared/telemetry/healthTally';

import { warnPlayback } from '@features/playback/domain/playbackError';

export type PrefetchFailureStage = 'resolve' | 'download' | 'swap';

export type QueueRestoreStage = 'fetch' | 'placeholder' | 'tracks' | 'rebuild' | 'native';

export type QueueRebuildRung = 'natural' | 'play_order' | 'exhausted';

const HEALTH_KEYS = [
  'prefetch_ok',
  'prefetch_failed_resolve',
  'prefetch_failed_download',
  'prefetch_failed_swap',
  'presign_ok',
  'presign_failed',
  'queue_rebuild_natural',
  'queue_rebuild_play_order',
  'queue_rebuild_exhausted',
  'playback_failed_network',
  'playback_failed_auth',
  'playback_failed_not_found',
  'playback_failed_decode',
  'playback_failed_queue_out_of_sync',
  'playback_failed_queue_update_failed',
  'playback_failed_unknown',
  'audio_recovery_failed',
  'queue_save_failed',
  'queue_restore_failed_fetch',
  'queue_restore_failed_placeholder',
  'queue_restore_failed_tracks',
  'queue_restore_failed_rebuild',
  'queue_restore_failed_native',
  'queue_state_rejected',
] as const;

type HealthKey = (typeof HEALTH_KEYS)[number];

const health = createHealthTally<HealthKey>({
  eventType: 'playback_health',
  keys: HEALTH_KEYS,
  onFlushFailure: (err: unknown) => {
    warnPlayback('failed to record playback health', {}, err);
  },
});

export const PLAYBACK_HEALTH_BATCH = HEALTH_BATCH;

const { count } = health;

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

export const flushPlaybackHealth = health.flush;

export const _resetPlaybackHealthForTest = health.reset;
