import { correlationIdOf } from '@shared/failure';

import type { EventPayload } from '@shared/telemetry/core/discoveryEvent';
import type { ClientEvent } from '@shared/telemetry/core/operationalEvent';
import type { TelemetryClock } from '@shared/telemetry/ports/telemetry';

const MAX_MESSAGE_LENGTH = 500;
const MAX_STACK_LENGTH = 4_000;
const DEDUPE_WINDOW_MS = 60_000;
const DEDUPE_STACK_HEAD_LENGTH = 200;
const MAX_TRACKED_ERRORS = 50;

type ClientErrorSource =
  'uncaught' | 'unhandled_rejection' | 'boundary' | 'query' | 'mutation' | 'playback';

export type ChainUncaughtErrors = (onError: (error: unknown) => void) => void;

export type ChainUnhandledRejections = (onRejection: (reason: unknown) => void) => void;

type ClientErrorReporterDeps = {
  clock: TelemetryClock;
  appVersion: () => string;
  enqueueCritical: (event: ClientEvent) => Promise<void>;
};

type ClientErrorPayload = EventPayload;

type SeenError = {
  windowStart: number;
  suppressed: number;
  payload: ClientErrorPayload;
  flushTimer: ReturnType<typeof setTimeout> | undefined;
};

type Occurrence = { key: string; now: number; payload: ClientErrorPayload };

type RejectionTrackingOptions = {
  allRejections: boolean;
  onUnhandled: (id: number, error: unknown) => void;
  onHandled: (id: number) => void;
};

type HermesRuntime = {
  enablePromiseRejectionTracker?: (options: RejectionTrackingOptions) => void;
};

function trimmed(value: string, maxLength: number): string {
  return value.length > maxLength ? `${value.slice(0, maxLength)}…` : value;
}

function messageAndStackOf(error: unknown): { message: string; stack: string | undefined } {
  if (error instanceof Error) {
    return { message: error.message, stack: error.stack };
  }
  return { message: String(error), stack: undefined };
}

function dedupeKey(source: ClientErrorSource, message: string, stack: string | undefined): string {
  return [
    source,
    trimmed(message, MAX_MESSAGE_LENGTH),
    (stack ?? '').slice(0, DEDUPE_STACK_HEAD_LENGTH),
  ].join('\n');
}

function isInsideWindow(seen: SeenError, now: number): boolean {
  const elapsed = now - seen.windowStart;
  return elapsed >= 0 && elapsed < DEDUPE_WINDOW_MS;
}

function optionalPayloadFields(error: unknown, stack: string | undefined): ClientErrorPayload {
  const correlationId = correlationIdOf(error);
  return {
    ...(stack === undefined ? undefined : { stack: trimmed(stack, MAX_STACK_LENGTH) }),
    ...(correlationId === undefined ? undefined : { correlation_id: correlationId }),
  };
}

function hermesRuntime(): HermesRuntime | undefined {
  return (globalThis as { HermesInternal?: HermesRuntime }).HermesInternal;
}

export function createClientErrorReporter(deps: ClientErrorReporterDeps) {
  const { clock, appVersion, enqueueCritical } = deps;
  const seenErrors = new Map<string, SeenError>();
  let installed = false;

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

  function forgetExpiredErrors(now: number): void {
    for (const [key, seen] of seenErrors) {
      if (!isInsideWindow(seen, now)) flushSuppressedRepeats(key, seen);
    }
  }

  function evictOneToMakeRoom(): void {
    if (seenErrors.size < MAX_TRACKED_ERRORS) return;
    const quietFirst = [...seenErrors].sort(
      ([, a], [, b]) => Number(a.suppressed !== 0) - Number(b.suppressed !== 0),
    );
    for (const [key, seen] of quietFirst.slice(0, 1)) flushSuppressedRepeats(key, seen);
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

  function reportClientError(error: unknown, source: ClientErrorSource): void {
    const { message, stack } = messageAndStackOf(error);
    const payload = {
      source,
      message: trimmed(message, MAX_MESSAGE_LENGTH),
      app_version: appVersion(),
      ...optionalPayloadFields(error, stack),
    };
    const key = dedupeKey(source, message, stack);
    const suppressedRepeats = suppressedRepeatsBefore({ key, now: clock.now(), payload });
    if (suppressedRepeats === undefined) return;
    enqueueReport(payload, suppressedRepeats);
  }

  function trackUnhandledRejections(chainUnhandledRejections: ChainUnhandledRejections): void {
    if (__DEV__) return;
    hermesRuntime()?.enablePromiseRejectionTracker?.({
      allRejections: true,
      onUnhandled: (_id, error) => reportClientError(error, 'unhandled_rejection'),
      onHandled: () => undefined,
    });
    chainUnhandledRejections((reason) => reportClientError(reason, 'unhandled_rejection'));
  }

  function installGlobalErrorReporting(
    chainUncaughtErrors: ChainUncaughtErrors,
    chainUnhandledRejections: ChainUnhandledRejections,
  ): void {
    if (installed) return;
    installed = true;
    chainUncaughtErrors((error) => reportClientError(error, 'uncaught'));
    trackUnhandledRejections(chainUnhandledRejections);
  }

  function dispose(): void {
    seenErrors.forEach((seen) => clearTimeout(seen.flushTimer));
    seenErrors.clear();
  }

  return { reportClientError, installGlobalErrorReporting, dispose };
}
