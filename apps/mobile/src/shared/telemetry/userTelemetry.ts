import { recordEvent } from './recordEvent';

const MAX_TRIMMED_LENGTH = 300;

function trimmed(value: string): string {
  if (value.length <= MAX_TRIMMED_LENGTH) return value;
  const cut = value.slice(0, MAX_TRIMMED_LENGTH);
  const lastUnit = cut.charCodeAt(MAX_TRIMMED_LENGTH - 1);
  const isHighSurrogate = lastUnit >= 0xd800 && lastUnit <= 0xdbff;
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

export function recordUserAction(payload: UserActionPayload): void {
  const { error, ...rest } = payload;
  recordEvent({
    type: 'user_action',
    payload: { ...rest, ...(error === undefined ? {} : { error: trimmed(error) }) },
  }).catch(() => undefined);
}

export type FailureShownPayload = {
  surface: string;
  message: string;
  track_id?: string;
};

export function recordFailureShown(payload: FailureShownPayload): void {
  recordEvent({
    type: 'failure_shown',
    payload: { ...payload, message: trimmed(payload.message) },
  }).catch(() => undefined);
}
