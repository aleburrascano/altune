export {
  installGlobalErrorReporting,
  reportClientError,
  _resetGlobalErrorReportingForTest,
} from '@shared/telemetry/shell/clientErrorReporting';
export type { ChainUncaughtErrors } from '@shared/telemetry/shell/clientErrorReporting';
export { HEALTH_BATCH, createHealthTally } from '@shared/telemetry/shell/healthTally';
export { followSignedInUser } from '@shared/telemetry/shell/outboxOwner';
export {
  enqueueCritical,
  setOutboxOwner,
  _resetOutboxForTest,
  flushOutbox,
} from '@shared/telemetry/outbox';
export { recordEvent } from '@shared/telemetry/shell/recordEvent';
export type {
  EventPayload,
  DiscoveryEvent,
  DiscoveryEventType,
} from '@shared/telemetry/shell/recordEvent';
export { useRecordEvent } from '@shared/telemetry/shell/useRecordEvent';
export { recordFailureShown, recordUserAction } from '@shared/telemetry/userTelemetry';
export type { UserActionPayload } from '@shared/telemetry/userTelemetry';
export { getSessionId } from '@shared/telemetry/eventSession';
export type { OutboxEntry } from '@shared/telemetry/shell/outboxEntries';
export { capOutbox, dedupeById, withEnvelope } from '@shared/telemetry/shell/outboxEntries';
export {
  loadPersistedOutbox,
  persistOutbox,
  setOutboxFileStore,
} from '@shared/telemetry/outboxStore';
