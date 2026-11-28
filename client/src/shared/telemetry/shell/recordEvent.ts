import {
  isOperationalEvent,
  withTopLevelCorrelationId,
  type ClientEvent,
} from '@shared/telemetry/core/operationalEvent';
import type { SendEvent } from '@shared/telemetry/ports/telemetry';

export type RecordEvent = (event: ClientEvent) => Promise<void>;

export function createRecordEvent(send: SendEvent, sessionId: () => string): RecordEvent {
  return async (event) => {
    const withSession = { ...event, payload: { ...event.payload, session_id: sessionId() } };
    await send(
      isOperationalEvent(withSession) ? withTopLevelCorrelationId(withSession) : withSession,
    );
  };
}
