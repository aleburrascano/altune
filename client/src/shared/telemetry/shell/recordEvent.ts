import type { DiscoveryEvent } from '@shared/telemetry/core/discoveryEvent';
import type { SendEvent } from '@shared/telemetry/ports/telemetry';

export type RecordEvent = (event: DiscoveryEvent) => Promise<void>;

export function createRecordEvent(send: SendEvent, sessionId: () => string): RecordEvent {
  return (event) => send({ ...event, payload: { ...event.payload, session_id: sessionId() } });
}
