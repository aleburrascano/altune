export {
  installGlobalErrorReporting,
  reportClientError,
  enqueueCritical,
  flushOutbox,
  followSignedInUser,
  getSessionId,
  installTelemetry,
  recordEvent,
  recordFailureShown,
  recordUserAction,
  stopTelemetry,
} from '@shared/telemetry/shell/currentTelemetry';
export { createTelemetry } from '@shared/telemetry/shell/telemetry';
export type { Telemetry } from '@shared/telemetry/shell/telemetry';
export { createAppTelemetry } from '@shared/telemetry/shell/appTelemetry';
export { createOutboxStore } from '@shared/telemetry/adapters/outboxStore';
export type {
  ChainUncaughtErrors,
  ChainUnhandledRejections,
} from '@shared/telemetry/shell/clientErrorReporting';
export { HEALTH_BATCH, createHealthTally } from '@shared/telemetry/shell/healthTally';
export type {
  EventPayload,
  DiscoveryEvent,
  DiscoveryEventType,
} from '@shared/telemetry/core/discoveryEvent';
export type {
  ClientEvent,
  ClientEventType,
  OperationalEvent,
  OperationalEventType,
} from '@shared/telemetry/core/operationalEvent';
export type { UserActionPayload } from '@shared/telemetry/shell/userTelemetry';
export type { OutboxEntry } from '@shared/telemetry/core/outboxEntries';
export { capOutbox, dedupeById, withEnvelope } from '@shared/telemetry/core/outboxEntries';
export { useRecordEvent } from '@shared/telemetry/shell/useRecordEvent';
export type { OutboxStore, TelemetryClock, TelemetryDeps } from '@shared/telemetry/ports/telemetry';
