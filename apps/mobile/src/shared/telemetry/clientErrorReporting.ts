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

type SeenError = { windowStart: number; suppressed: number };

const seenErrors = new Map<string, SeenError>();

function dedupeKey(source: ClientErrorSource, message: string, stack: string | undefined): string {
  return [source, message, (stack ?? '').slice(0, DEDUPE_STACK_HEAD_LENGTH)].join('\n');
}

function forgetExpiredErrors(now: number): void {
  for (const [key, seen] of seenErrors) {
    if (now - seen.windowStart >= DEDUPE_WINDOW_MS) seenErrors.delete(key);
  }
  if (seenErrors.size < MAX_TRACKED_ERRORS) return;
  const oldest = seenErrors.keys().next();
  if (!oldest.done) seenErrors.delete(oldest.value);
}

function isInsideWindow(seen: SeenError | undefined, now: number): boolean {
  return seen !== undefined && now - seen.windowStart < DEDUPE_WINDOW_MS;
}

function suppressedRepeatsBefore(key: string, now: number): number | undefined {
  const seen = seenErrors.get(key);
  if (seen !== undefined && isInsideWindow(seen, now)) {
    seen.suppressed += 1;
    return undefined;
  }
  forgetExpiredErrors(now);
  seenErrors.set(key, { windowStart: now, suppressed: 0 });
  return seen?.suppressed ?? 0;
}

function optionalPayloadFields(
  stack: string | undefined,
  suppressedRepeats: number,
): Record<string, unknown> {
  return {
    ...(stack === undefined ? {} : { stack: trimmed(stack, MAX_STACK_LENGTH) }),
    ...(suppressedRepeats > 0 && { suppressed_repeats: suppressedRepeats }),
  };
}

export function reportClientError(error: unknown, source: ClientErrorSource): void {
  const { message, stack } = messageAndStackOf(error);
  const suppressedRepeats = suppressedRepeatsBefore(dedupeKey(source, message, stack), Date.now());
  if (suppressedRepeats === undefined) return;
  const payload = {
    source,
    message: trimmed(message, MAX_MESSAGE_LENGTH),
    app_version: appVersion(),
    ...optionalPayloadFields(stack, suppressedRepeats),
  };
  void enqueueCritical({ type: 'client_error', payload });
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
  seenErrors.clear();
}
