import { ApiError, correlationIdOf } from '@shared/errors';
import type { TrackId } from '@shared/api-client/ids';
import { enqueueCritical } from '@shared/telemetry/outbox';

export type RetryEntryPoint =
  'library_row' | 'detail' | 'album_row' | 'artist_row' | 'playlist' | 'featuring';

export type RetrySkipReason = 'already_running' | 'unsafe_id' | 'signed_out';

export type RetryRequestOutcome =
  | { kind: 'sent' }
  | { kind: 'skipped'; reason: RetrySkipReason }
  | { kind: 'succeeded' }
  | { kind: 'failed'; status?: number; correlationId?: string };

export type StatusChangeSource = 'optimistic' | 'response' | 'sse' | 'poll';

function recordAcquisitionUi(
  trackId: TrackId,
  action: string,
  payload: Record<string, unknown>,
): void {
  void enqueueCritical({
    type: 'acquisition_ui',
    payload: { track_id: trackId, action, ...payload },
  });
}

function entryPointPayload(entryPoint: RetryEntryPoint | undefined): Record<string, unknown> {
  return entryPoint === undefined ? {} : { entry_point: entryPoint };
}

export function recordRetryTapped(trackId: TrackId, entryPoint: RetryEntryPoint | undefined): void {
  recordAcquisitionUi(trackId, 'retry_tapped', entryPointPayload(entryPoint));
}

function failedPayload(status?: number, correlationId?: string): Record<string, unknown> {
  return {
    outcome: 'failed',
    ...(status === undefined ? {} : { status }),
    ...(correlationId === undefined ? {} : { correlation_id: correlationId }),
  };
}

function outcomePayload(outcome: RetryRequestOutcome): Record<string, unknown> {
  switch (outcome.kind) {
    case 'skipped':
      return { outcome: 'skipped', reason: outcome.reason };
    case 'failed':
      return failedPayload(outcome.status, outcome.correlationId);
    default:
      return { outcome: outcome.kind };
  }
}

export function recordRetryRequest(
  trackId: TrackId,
  entryPoint: RetryEntryPoint | undefined,
  outcome: RetryRequestOutcome,
): void {
  recordAcquisitionUi(trackId, 'retry_request', {
    ...entryPointPayload(entryPoint),
    ...outcomePayload(outcome),
  });
}

export function retryFailureOutcome(error: unknown): RetryRequestOutcome {
  const correlationId = correlationIdOf(error);
  return {
    kind: 'failed',
    ...(error instanceof ApiError ? { status: error.status } : {}),
    ...(correlationId === undefined ? {} : { correlationId }),
  };
}

const lastShownFailure = new Map<TrackId, string>();

export function recordFailureShownOnce(trackId: TrackId, message: string): void {
  if (lastShownFailure.get(trackId) === message) return;
  lastShownFailure.set(trackId, message);
  recordAcquisitionUi(trackId, 'failure_shown', { message });
}

export function recordStatusChanged(
  trackId: TrackId,
  from: string | null,
  to: string,
  source: StatusChangeSource,
): void {
  recordAcquisitionUi(trackId, 'status_changed', { from, to, source });
}

export function _resetAcquisitionTelemetryForTest(): void {
  lastShownFailure.clear();
}
