import ErrorUtils from 'react-native/Libraries/vendor/core/ErrorUtils';

import { appVersion } from '@shared/device/device';

import { enqueueCritical } from './outbox';

const MAX_MESSAGE_LENGTH = 500;
const MAX_STACK_LENGTH = 4_000;
const DEDUPE_WINDOW_MS = 60_000;
const DEDUPE_STACK_HEAD_LENGTH = 200;
const MAX_TRACKED_ERRORS = 50;

export type ClientErrorSource =
  'uncaught' | 'unhandled_rejection' | 'boundary' | 'query' | 'mutation';

function trimmed(value: string, maxLength: number): string {
  return value.length > maxLength ? `${value.slice(0, maxLength)}…` : value;
}

function messageAndStackOf(error: unknown): { message: string; stack: string | undefined } {
  if (error instanceof Error) {
    return { message: error.message, stack: error.stack };
  }
  return { message: String(error), stack: undefined };
}

type ClientErrorPayload = Record<string, unknown>;

type SeenError = {
  windowStart: number;
  suppressed: number;
  payload: ClientErrorPayload;
  flushTimer: ReturnType<typeof setTimeout> | undefined;
};

const seenErrors = new Map<string, SeenError>();

function dedupeKey(source: ClientErrorSource, message: string, stack: string | undefined): string {
  return [
    source,
    trimmed(message, MAX_MESSAGE_LENGTH),
    (stack ?? '').slice(0, DEDUPE_STACK_HEAD_LENGTH),
  ].join('\n');
}

function enqueueReport(payload: ClientErrorPayload, suppressedRepeats: number): void {
  const withCount =
    suppressedRepeats > 0 ? { ...payload, suppressed_repeats: suppressedRepeats } : payload;
  void enqueueCritical({ type: 'client_error', payload: withCount });
}

function forgetError(key: string, seen: SeenError): void {
  clearTimeout(seen.flushTimer);
  seenErrors.delete(key);
}

function flushSuppressedRepeats(key: string, seen: SeenError): void {
  forgetError(key, seen);
  if (seen.suppressed > 0) enqueueReport(seen.payload, seen.suppressed);
}

function isInsideWindow(seen: SeenError, now: number): boolean {
  const elapsed = now - seen.windowStart;
  return elapsed >= 0 && elapsed < DEDUPE_WINDOW_MS;
}

function forgetExpiredErrors(now: number): void {
  for (const [key, seen] of seenErrors) {
    if (!isInsideWindow(seen, now)) flushSuppressedRepeats(key, seen);
  }
}

function evictOneToMakeRoom(): void {
  if (seenErrors.size < MAX_TRACKED_ERRORS) return;
  const entries = [...seenErrors];
  const [key, seen] =
    entries.find(([, candidate]) => candidate.suppressed === 0) ?? entries[0] ?? [];
  if (key !== undefined && seen !== undefined) flushSuppressedRepeats(key, seen);
}

function flushWhenWindowEnds(key: string, seen: SeenError, now: number): void {
  if (seen.flushTimer !== undefined) return;
  const remaining = seen.windowStart + DEDUPE_WINDOW_MS - now;
  seen.flushTimer = setTimeout(() => flushSuppressedRepeats(key, seen), remaining);
}

function countSuppressedRepeat(key: string, seen: SeenError, now: number): void {
  seen.suppressed += 1;
  flushWhenWindowEnds(key, seen, now);
}

type Occurrence = { key: string; now: number; payload: ClientErrorPayload };

function startWindow({ key, now, payload }: Occurrence): void {
  seenErrors.set(key, { windowStart: now, suppressed: 0, payload, flushTimer: undefined });
}

function reopenWindow(seen: SeenError | undefined, occurrence: Occurrence): number {
  if (seen !== undefined) forgetError(occurrence.key, seen);
  forgetExpiredErrors(occurrence.now);
  if (seen === undefined) evictOneToMakeRoom();
  startWindow(occurrence);
  return seen?.suppressed ?? 0;
}

function suppressedRepeatsBefore(occurrence: Occurrence): number | undefined {
  const seen = seenErrors.get(occurrence.key);
  if (seen === undefined || !isInsideWindow(seen, occurrence.now)) {
    return reopenWindow(seen, occurrence);
  }
  countSuppressedRepeat(occurrence.key, seen, occurrence.now);
  return undefined;
}

function optionalPayloadFields(stack: string | undefined): ClientErrorPayload {
  return stack === undefined ? {} : { stack: trimmed(stack, MAX_STACK_LENGTH) };
}

export function reportClientError(error: unknown, source: ClientErrorSource): void {
  const { message, stack } = messageAndStackOf(error);
  const payload = {
    source,
    message: trimmed(message, MAX_MESSAGE_LENGTH),
    app_version: appVersion(),
    ...optionalPayloadFields(stack),
  };
  const key = dedupeKey(source, message, stack);
  const suppressedRepeats = suppressedRepeatsBefore({ key, now: Date.now(), payload });
  if (suppressedRepeats === undefined) return;
  enqueueReport(payload, suppressedRepeats);
}

let installed = false;

function chainUncaughtHandler(): void {
  const previousHandler = ErrorUtils.getGlobalHandler();
  ErrorUtils.setGlobalHandler((error, isFatal) => {
    reportClientError(error, 'uncaught');
    previousHandler(error, isFatal);
  });
}

type RejectionTrackingOptions = {
  allRejections: boolean;
  onUnhandled: (id: number, error: unknown) => void;
  onHandled: (id: number) => void;
};

type HermesRuntime = {
  enablePromiseRejectionTracker?: (options: RejectionTrackingOptions) => void;
};

function hermesRuntime(): HermesRuntime | undefined {
  return (globalThis as { HermesInternal?: HermesRuntime }).HermesInternal;
}

function trackUnhandledRejections(): void {
  if (__DEV__) return;
  hermesRuntime()?.enablePromiseRejectionTracker?.({
    allRejections: true,
    onUnhandled: (_id, error) => reportClientError(error, 'unhandled_rejection'),
    onHandled: () => undefined,
  });
}

export function installGlobalErrorReporting(): void {
  if (installed) return;
  installed = true;
  chainUncaughtHandler();
  trackUnhandledRejections();
}

export function _resetGlobalErrorReportingForTest(): void {
  installed = false;
  seenErrors.forEach((seen) => clearTimeout(seen.flushTimer));
  seenErrors.clear();
}
