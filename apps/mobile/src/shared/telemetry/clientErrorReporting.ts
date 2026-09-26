import Constants from 'expo-constants';
import ErrorUtils from 'react-native/Libraries/vendor/core/ErrorUtils';

import { enqueueCritical } from './outbox';

const MAX_MESSAGE_LENGTH = 500;
const MAX_STACK_LENGTH = 4_000;

export type ClientErrorSource = 'uncaught' | 'unhandled_rejection' | 'boundary';

function trimmed(value: string, maxLength: number): string {
  return value.length > maxLength ? `${value.slice(0, maxLength)}…` : value;
}

function appVersion(): string {
  return Constants.expoConfig?.version ?? 'dev';
}

function messageAndStackOf(error: unknown): { message: string; stack: string | undefined } {
  if (error instanceof Error) {
    return { message: error.message, stack: error.stack };
  }
  return { message: String(error), stack: undefined };
}

function clientErrorPayload(error: unknown, source: ClientErrorSource): Record<string, unknown> {
  const { message, stack } = messageAndStackOf(error);
  return {
    source,
    message: trimmed(message, MAX_MESSAGE_LENGTH),
    ...(stack === undefined ? {} : { stack: trimmed(stack, MAX_STACK_LENGTH) }),
    app_version: appVersion(),
  };
}

export function reportClientError(error: unknown, source: ClientErrorSource): void {
  void enqueueCritical({ type: 'client_error', payload: clientErrorPayload(error, source) });
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
}
