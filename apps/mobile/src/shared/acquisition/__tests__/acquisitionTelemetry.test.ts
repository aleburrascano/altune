import { ApiError } from '@shared/errors';
import { asTrackId } from '@shared/api-client/ids';
import { enqueueCritical } from '@shared/telemetry/outbox';

import {
  _resetAcquisitionTelemetryForTest,
  recordFailureShownOnce,
  recordRetryRequest,
  recordRetryTapped,
  recordStatusChanged,
  retryFailureOutcome,
} from '../acquisitionTelemetry';

jest.mock('@shared/telemetry/outbox', () => ({ enqueueCritical: jest.fn() }));

const enqueueCriticalMock = enqueueCritical as jest.MockedFunction<typeof enqueueCritical>;

function lastPayload(): Record<string, unknown> {
  const call = enqueueCriticalMock.mock.calls.at(-1);
  return (call?.[0]?.payload ?? {}) as Record<string, unknown>;
}

beforeEach(() => {
  enqueueCriticalMock.mockReset().mockResolvedValue(undefined);
  _resetAcquisitionTelemetryForTest();
});

describe('recordRetryTapped', () => {
  it('carries the track id and action, with no entry point when none is given', () => {
    recordRetryTapped(asTrackId('t-1'), undefined);

    expect(enqueueCriticalMock).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'acquisition_ui' }),
    );
    expect(lastPayload()).toEqual({ track_id: 't-1', action: 'retry_tapped' });
  });

  it('carries the entry point when one is given', () => {
    recordRetryTapped(asTrackId('t-1'), 'library_row');

    expect(lastPayload()).toEqual({
      track_id: 't-1',
      action: 'retry_tapped',
      entry_point: 'library_row',
    });
  });
});

describe('recordRetryRequest', () => {
  it('records a sent request', () => {
    recordRetryRequest(asTrackId('t-1'), 'library_row', { kind: 'sent' });

    expect(lastPayload()).toEqual({
      track_id: 't-1',
      action: 'retry_request',
      entry_point: 'library_row',
      outcome: 'sent',
    });
  });

  it('records a succeeded request', () => {
    recordRetryRequest(asTrackId('t-1'), undefined, { kind: 'succeeded' });

    expect(lastPayload()).toEqual({
      track_id: 't-1',
      action: 'retry_request',
      outcome: 'succeeded',
    });
  });

  it('records a skip with its reason', () => {
    recordRetryRequest(asTrackId('t-1'), 'featuring', { kind: 'skipped', reason: 'unsafe_id' });

    expect(lastPayload()).toEqual({
      track_id: 't-1',
      action: 'retry_request',
      entry_point: 'featuring',
      outcome: 'skipped',
      reason: 'unsafe_id',
    });
  });

  it('records a failure with status and correlation id when both are present', () => {
    recordRetryRequest(asTrackId('t-1'), undefined, {
      kind: 'failed',
      status: 503,
      correlationId: 'corr-1',
    });

    expect(lastPayload()).toEqual({
      track_id: 't-1',
      action: 'retry_request',
      outcome: 'failed',
      status: 503,
      correlation_id: 'corr-1',
    });
  });

  it('records a failure with neither status nor correlation id when absent', () => {
    recordRetryRequest(asTrackId('t-1'), undefined, { kind: 'failed' });

    expect(lastPayload()).toEqual({ track_id: 't-1', action: 'retry_request', outcome: 'failed' });
  });
});

describe('retryFailureOutcome', () => {
  it('pulls the status off an ApiError', () => {
    const outcome = retryFailureOutcome(new ApiError(500, 'nope'));

    expect(outcome).toEqual({ kind: 'failed', status: 500 });
  });

  it('pulls the correlation id off an ApiError that carries one', () => {
    const outcome = retryFailureOutcome(new ApiError(503, 'nope', undefined, 'corr-9'));

    expect(outcome).toEqual({ kind: 'failed', status: 503, correlationId: 'corr-9' });
  });

  it('carries no status for a non-ApiError', () => {
    const outcome = retryFailureOutcome(new Error('boom'));

    expect(outcome).toEqual({ kind: 'failed' });
  });
});

describe('recordFailureShownOnce', () => {
  it('records the first time a message is shown for a track', () => {
    recordFailureShownOnce(asTrackId('t-1'), 'No source found');

    expect(lastPayload()).toEqual({
      track_id: 't-1',
      action: 'failure_shown',
      message: 'No source found',
    });
  });

  it('does not repeat the same message for the same track', () => {
    recordFailureShownOnce(asTrackId('t-1'), 'No source found');
    recordFailureShownOnce(asTrackId('t-1'), 'No source found');

    expect(enqueueCriticalMock).toHaveBeenCalledTimes(1);
  });

  it('records again when the message for the same track changes', () => {
    recordFailureShownOnce(asTrackId('t-1'), 'No source found');
    recordFailureShownOnce(asTrackId('t-1'), 'Provider unavailable');

    expect(enqueueCriticalMock).toHaveBeenCalledTimes(2);
    expect(lastPayload()).toEqual({
      track_id: 't-1',
      action: 'failure_shown',
      message: 'Provider unavailable',
    });
  });

  it('trims a long message so the event stays under the server payload cap', () => {
    recordFailureShownOnce(asTrackId('t-1'), 'x'.repeat(10_000));

    expect(lastPayload().message).toBe(`${'x'.repeat(500)}…`);
  });
});

describe('recordStatusChanged', () => {
  it('carries from, to and source', () => {
    recordStatusChanged(asTrackId('t-1'), 'failed', 'pending', 'optimistic');

    expect(lastPayload()).toEqual({
      track_id: 't-1',
      action: 'status_changed',
      from: 'failed',
      to: 'pending',
      source: 'optimistic',
    });
  });
});
