import { ApiError } from '@shared/failure/errors';
import { asTrackId } from '@shared/apiClient/ids';
import { flushOutbox } from '@shared/telemetry/outbox';
import { signInTestUser } from '@testing/signInTestUser';

import {
  _resetAcquisitionTelemetryForTest,
  recordFailureShownOnce,
  recordRetryRequest,
  recordRetryTapped,
  recordStatusChanged,
  retryFailureOutcome,
} from '@shared/acquisition/acquisitionTelemetry';

const { __http } = require('@testing/doubles/fetch.js');

jest.mock('@supabase/supabase-js', () => require('@testing/doubles/supabase-js.js'));

const TRIM_LIMIT = 500;
const TWO_MESSAGES = 2;

interface SentEvent {
  type: string;
  payload: Record<string, unknown>;
}

interface HttpRequest {
  path: string;
  body: string;
}

function sentEvents(): SentEvent[] {
  return (__http.requests as HttpRequest[])
    .filter((request) => request.path === '/v1/discovery/events')
    .map((request) => JSON.parse(request.body) as SentEvent);
}

async function delivered(): Promise<SentEvent[]> {
  await flushOutbox();
  await new Promise((resolve) => setImmediate(resolve));
  return sentEvents();
}

async function lastPayload(): Promise<Record<string, unknown>> {
  const last = (await delivered()).at(-1);
  if (last === undefined) throw new Error('no event was delivered');
  const payload = { ...last.payload };
  delete payload.session_id;
  return payload;
}

beforeEach(async () => {
  await signInTestUser();
  __http.replyAll({ status: 202 });
  _resetAcquisitionTelemetryForTest();
});

describe('recordRetryTapped', () => {
  it('carries the track id and action, with no entry point when none is given', async () => {
    recordRetryTapped(asTrackId('t-1'), undefined);

    expect((await delivered())[0]?.type).toBe('acquisition_ui');
    expect(await lastPayload()).toEqual({ track_id: 't-1', action: 'retry_tapped' });
  });

  it('carries the entry point when one is given', async () => {
    recordRetryTapped(asTrackId('t-1'), 'library_row');

    expect(await lastPayload()).toEqual({
      track_id: 't-1',
      action: 'retry_tapped',
      entry_point: 'library_row',
    });
  });
});

describe('recordRetryRequest', () => {
  it('records a sent request', async () => {
    recordRetryRequest(asTrackId('t-1'), 'library_row', { kind: 'sent' });

    expect(await lastPayload()).toEqual({
      track_id: 't-1',
      action: 'retry_request',
      entry_point: 'library_row',
      outcome: 'sent',
    });
  });

  it('records a succeeded request', async () => {
    recordRetryRequest(asTrackId('t-1'), undefined, { kind: 'succeeded' });

    expect(await lastPayload()).toEqual({
      track_id: 't-1',
      action: 'retry_request',
      outcome: 'succeeded',
    });
  });

  it('records a skip with its reason', async () => {
    recordRetryRequest(asTrackId('t-1'), 'featuring', { kind: 'skipped', reason: 'unsafe_id' });

    expect(await lastPayload()).toEqual({
      track_id: 't-1',
      action: 'retry_request',
      entry_point: 'featuring',
      outcome: 'skipped',
      reason: 'unsafe_id',
    });
  });

  it('records a failure with status and correlation id when both are present', async () => {
    recordRetryRequest(asTrackId('t-1'), undefined, {
      kind: 'failed',
      status: 503,
      correlationId: 'corr-1',
    });

    expect(await lastPayload()).toEqual({
      track_id: 't-1',
      action: 'retry_request',
      outcome: 'failed',
      status: 503,
      correlation_id: 'corr-1',
    });
  });

  it('records a failure with neither status nor correlation id when absent', async () => {
    recordRetryRequest(asTrackId('t-1'), undefined, { kind: 'failed' });

    expect(await lastPayload()).toEqual({
      track_id: 't-1',
      action: 'retry_request',
      outcome: 'failed',
    });
  });
});

describe('retryFailureOutcome', () => {
  it('pulls the status off an ApiError', async () => {
    const outcome = retryFailureOutcome(new ApiError(500, 'nope'));

    expect(outcome).toEqual({ kind: 'failed', status: 500 });
  });

  it('pulls the correlation id off an ApiError that carries one', async () => {
    const outcome = retryFailureOutcome(new ApiError(503, 'nope', undefined, 'corr-9'));

    expect(outcome).toEqual({ kind: 'failed', status: 503, correlationId: 'corr-9' });
  });

  it('carries no status for a non-ApiError', async () => {
    const outcome = retryFailureOutcome(new Error('boom'));

    expect(outcome).toEqual({ kind: 'failed' });
  });
});

describe('recordFailureShownOnce', () => {
  it('records the first time a message is shown for a track', async () => {
    recordFailureShownOnce(asTrackId('t-1'), 'No source found');

    expect(await lastPayload()).toEqual({
      track_id: 't-1',
      action: 'failure_shown',
      message: 'No source found',
    });
  });

  it('does not repeat the same message for the same track', async () => {
    recordFailureShownOnce(asTrackId('t-1'), 'No source found');
    recordFailureShownOnce(asTrackId('t-1'), 'No source found');

    expect(await delivered()).toHaveLength(1);
  });

  it('records again when the message for the same track changes', async () => {
    recordFailureShownOnce(asTrackId('t-1'), 'No source found');
    recordFailureShownOnce(asTrackId('t-1'), 'Provider unavailable');

    expect(await delivered()).toHaveLength(TWO_MESSAGES);
    expect(await lastPayload()).toEqual({
      track_id: 't-1',
      action: 'failure_shown',
      message: 'Provider unavailable',
    });
  });

  it('trims a long message so the event stays under the server payload cap', async () => {
    recordFailureShownOnce(asTrackId('t-1'), 'x'.repeat(10_000));

    expect((await lastPayload()).message).toBe(`${'x'.repeat(TRIM_LIMIT)}…`);
  });
});

describe('recordStatusChanged', () => {
  it('carries from, to and source', async () => {
    recordStatusChanged(asTrackId('t-1'), 'failed', 'pending', 'optimistic');

    expect(await lastPayload()).toEqual({
      track_id: 't-1',
      action: 'status_changed',
      from: 'failed',
      to: 'pending',
      source: 'optimistic',
    });
  });
});
