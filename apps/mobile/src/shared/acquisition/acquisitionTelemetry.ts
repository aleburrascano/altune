import { ApiError, correlationIdOf } from '@shared/errors';
import type { TrackId } from '@shared/api-client/ids';
import { enqueueCritical } from '@shared/telemetry/outbox';

// The 2026-09-25 incident (#2853) showed Retry going failed -> pending -> failed with no
// request reaching the server, and no way to tell from the client alone whether the tap
// never called through, the call was skipped by a guard, or the request itself vanished.
// These events answer that from the outbox: they are label-critical (retried, persisted
// across a restart) rather than best-effort like the health tallies, because the whole
// point is to survive exactly the failure that made the incident hard to see.

export type RetryEntryPoint =
  | 'library_row'
  | 'detail'
  | 'album_row'
  | 'artist_row'
  | 'playlist'
  | 'featuring';

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

function outcomePayload(outcome: RetryRequestOutcome): Record<string, unknown> {
  switch (outcome.kind) {
    case 'sent':
      return { outcome: 'sent' };
    case 'skipped':
      return { outcome: 'skipped', reason: outcome.reason };
    case 'succeeded':
      return { outcome: 'succeeded' };
    case 'failed':
      return {
        outcome: 'failed',
        ...(outcome.status === undefined ? {} : { status: outcome.status }),
        ...(outcome.correlationId === undefined ? {} : { correlation_id: outcome.correlationId }),
      };
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

/** Builds the failed outcome from a retry request's rejection, the same status/correlation-id pair failureLogFields draws for the log line. */
export function retryFailureOutcome(error: unknown): RetryRequestOutcome {
  const correlationId = correlationIdOf(error);
  return {
    kind: 'failed',
    ...(error instanceof ApiError ? { status: error.status } : {}),
    ...(correlationId === undefined ? {} : { correlationId }),
  };
}

const lastShownFailure = new Map<TrackId, string>();

/** Records `failure_shown` once per distinct message a track's failure banner displays; a
 * re-render with the same message (a list re-fetch, a sibling row remount) does not repeat it. */
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
