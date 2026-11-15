export {
  installGlobalErrorReporting,
  reportClientError,
  _resetGlobalErrorReportingForTest,
} from '@shared/telemetry/clientErrorReporting';
export type { ChainUncaughtErrors } from '@shared/telemetry/clientErrorReporting';
export { HEALTH_BATCH, createHealthTally } from '@shared/telemetry/healthTally';
export { followSignedInUser } from '@shared/telemetry/shell/outboxOwner';
export {
  enqueueCritical,
  setOutboxOwner,
  _resetOutboxForTest,
  flushOutbox,
} from '@shared/telemetry/outbox';
export { recordEvent } from '@shared/telemetry/recordEvent';
export type {
  EventPayload,
  DiscoveryEvent,
  DiscoveryEventType,
} from '@shared/telemetry/recordEvent';
export { useRecordEvent } from '@shared/telemetry/useRecordEvent';
export { recordFailureShown, recordUserAction } from '@shared/telemetry/userTelemetry';
export type { UserActionPayload } from '@shared/telemetry/userTelemetry';
export { getSessionId } from '@shared/telemetry/eventSession';
export type { OutboxEntry } from '@shared/telemetry/outboxEntries';
export { capOutbox, dedupeById, withEnvelope } from '@shared/telemetry/outboxEntries';
export {
  loadPersistedOutbox,
  persistOutbox,
  setOutboxFileStore,
} from '@shared/telemetry/outboxStore';
