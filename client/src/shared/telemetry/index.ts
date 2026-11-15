export {
  installGlobalErrorReporting,
  reportClientError,
} from '@shared/telemetry/clientErrorReporting';
export { HEALTH_BATCH, createHealthTally } from '@shared/telemetry/healthTally';
export { reportOtaLaunchFailures } from '@shared/telemetry/native/otaLaunchReport';
export { enqueueCritical, setOutboxOwner } from '@shared/telemetry/outbox';
export { recordEvent } from '@shared/telemetry/recordEvent';
export type { EventPayload } from '@shared/telemetry/recordEvent';
export { useRecordEvent } from '@shared/telemetry/useRecordEvent';
export { recordFailureShown, recordUserAction } from '@shared/telemetry/userTelemetry';
export type { UserActionPayload } from '@shared/telemetry/userTelemetry';
