import { createAppTelemetry } from '@shared/telemetry/shell/appTelemetry';
import type { Telemetry } from '@shared/telemetry/shell/telemetry';

let installed: Telemetry | undefined;

function current(): Telemetry {
  installed ??= createAppTelemetry();
  return installed;
}

export function installTelemetry(next: Telemetry): void {
  installed?.dispose();
  installed = next;
}

export function recordEvent(...args: Parameters<Telemetry['recordEvent']>) {
  return current().recordEvent(...args);
}

export function getSessionId(): string {
  return current().getSessionId();
}

export function enqueueCritical(...args: Parameters<Telemetry['enqueueCritical']>) {
  return current().enqueueCritical(...args);
}

export function flushOutbox(): Promise<void> {
  return current().flushOutbox();
}

export function reportClientError(...args: Parameters<Telemetry['reportClientError']>): void {
  current().reportClientError(...args);
}

export function installGlobalErrorReporting(
  ...args: Parameters<Telemetry['installGlobalErrorReporting']>
): void {
  current().installGlobalErrorReporting(...args);
}

export function recordUserAction(...args: Parameters<Telemetry['recordUserAction']>): void {
  current().recordUserAction(...args);
}

export function recordFailureShown(...args: Parameters<Telemetry['recordFailureShown']>): void {
  current().recordFailureShown(...args);
}

export function followSignedInUser(): () => void {
  return current().followSignedInUser();
}
