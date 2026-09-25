import Constants from 'expo-constants';
import ErrorUtils from 'react-native/Libraries/vendor/core/ErrorUtils';
import { enable as enableRejectionTracking } from 'promise/setimmediate/rejection-tracking';

import { enqueueCritical } from './outbox';

// Trimmed the way the discovery events payload cap (8 KiB total) expects a single field to
// stay well under it: a stack can run to tens of KB uncaught, and would alone blow the cap.
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

export function reportClientError(error: unknown, source: ClientErrorSource): void {
  const { message, stack } = messageAndStackOf(error);
  void enqueueCritical({
    type: 'client_error',
    payload: {
      source,
      message: trimmed(message, MAX_MESSAGE_LENGTH),
      ...(stack === undefined ? {} : { stack: trimmed(stack, MAX_STACK_LENGTH) }),
      app_version: appVersion(),
    },
  });
}

let installed = false;

/**
 * Wires uncaught JS errors and unhandled promise rejections into `client_error`. React error
 * boundaries (`ScreenBoundary`) report separately from `componentDidCatch`, the one path this
 * cannot see. Idempotent, and safe to call from module scope alongside the app's other
 * app-lifetime bridges (killSwitchPoll, useServerEvents).
 */
export function installGlobalErrorReporting(): void {
  if (installed) return;
  installed = true;

  const previousHandler = ErrorUtils.getGlobalHandler();
  ErrorUtils.setGlobalHandler((error, isFatal) => {
    reportClientError(error, 'uncaught');
    previousHandler(error, isFatal);
  });

  enableRejectionTracking({
    allRejections: true,
    onUnhandled: (_id, error) => {
      reportClientError(error, 'unhandled_rejection');
    },
  });
}

export function _resetGlobalErrorReportingForTest(): void {
  installed = false;
}
