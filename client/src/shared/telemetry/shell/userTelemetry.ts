import type { TelemetryLog } from '@shared/telemetry/ports/telemetry';
import type { RecordEvent } from '@shared/telemetry/shell/recordEvent';

const MAX_TRIMMED_LENGTH = 300;
const HIGH_SURROGATE_START = 0xd800;
const HIGH_SURROGATE_END = 0xdbff;

function trimmed(value: string): string {
  if (value.length <= MAX_TRIMMED_LENGTH) return value;
  const cut = value.slice(0, MAX_TRIMMED_LENGTH);
  const lastUnit = cut.charCodeAt(MAX_TRIMMED_LENGTH - 1);
  const isHighSurrogate = lastUnit >= HIGH_SURROGATE_START && lastUnit <= HIGH_SURROGATE_END;
  return isHighSurrogate ? cut.slice(0, MAX_TRIMMED_LENGTH - 1) : cut;
}

export type UserActionOutcome = 'tapped' | 'succeeded' | 'failed';

export type UserActionPayload = {
  action: string;
  outcome: UserActionOutcome;
  track_id?: string;
  status?: number;
  correlation_id?: string;
  error?: string;
};

type FailureShownPayload = {
  surface: string;
  message: string;
  track_id?: string;
};

type UserTelemetryDeps = { recordEvent: RecordEvent; log: TelemetryLog };

export function createUserTelemetry({ recordEvent, log }: UserTelemetryDeps) {
  function logFailure(error: unknown): void {
    log.warn('[telemetry] recording event failed', error);
  }

  function recordUserAction(payload: UserActionPayload): void {
    const { error, ...rest } = payload;
    recordEvent({
      type: 'user_action',
      payload: { ...rest, ...(error === undefined ? {} : { error: trimmed(error) }) },
    }).catch(logFailure);
  }

  function recordFailureShown(payload: FailureShownPayload): void {
    recordEvent({
      type: 'failure_shown',
      payload: { ...payload, message: trimmed(payload.message) },
    }).catch(logFailure);
  }

  return { recordUserAction, recordFailureShown };
}
